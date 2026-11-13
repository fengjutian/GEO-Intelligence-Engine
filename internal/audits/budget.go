package audits

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ensureAuditBudget provisions an audit-scoped monthly account, reserves the estimated
// cost atomically, and rejects the audit when the hard limit would be exceeded.
func ensureAuditBudget(ctx context.Context, tx *sql.Tx, tenantID, auditID string, budget, estimated int64, currency string) error {
	if budget <= 0 && estimated <= 0 {
		return nil
	}
	accountID, _ := uuid.NewV7()
	_, err := tx.ExecContext(ctx, `INSERT INTO budget_accounts (id,tenant_id,scope_type,scope_id,period_start,period_end,currency,soft_limit_microunits,hard_limit_microunits,reserved_microunits,settled_microunits)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'audit',UUID_TO_BIN(?),CURDATE(),CURDATE(),?,NULL,?,0,0)
ON DUPLICATE KEY UPDATE hard_limit_microunits=hard_limit_microunits`, accountID.String(), tenantID, auditID, currency, budget)
	if err != nil {
		return fmt.Errorf("provision audit budget account: %w", err)
	}
	var resolvedID string
	if err := tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM budget_accounts WHERE tenant_id=UUID_TO_BIN(?) AND scope_type='audit' AND scope_id=UUID_TO_BIN(?) AND period_start=CURDATE() AND period_end=CURDATE() AND currency=? FOR UPDATE`, tenantID, auditID, currency).Scan(&resolvedID); err != nil {
		return fmt.Errorf("read audit budget account: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE budget_accounts SET reserved_microunits=reserved_microunits+?
WHERE id=UUID_TO_BIN(?) AND (hard_limit_microunits=0 OR reserved_microunits+settled_microunits+? <= hard_limit_microunits)`, estimated, resolvedID, estimated)
	if err != nil {
		return fmt.Errorf("reserve audit budget: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrBudgetExceeded
	}
	reservationID, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO budget_reservations (id,tenant_id,budget_account_id,request_id,amount_microunits,status,expires_at,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,'reserved',UTC_TIMESTAMP(6)+INTERVAL 24 HOUR,UTC_TIMESTAMP(6))`, reservationID.String(), tenantID, resolvedID, auditID, estimated)
	if err != nil {
		return fmt.Errorf("create budget reservation: %w", err)
	}
	return nil
}

// settleSampleCost moves one sample's actual cost from reserved to settled and audits it.
func settleSampleCost(ctx context.Context, tx *sql.Tx, tenantID, auditID string, cost int64) error {
	if cost <= 0 {
		return nil
	}
	var reservationID, accountID, status string
	err := tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(r.id),BIN_TO_UUID(r.budget_account_id),r.status
FROM budget_reservations r WHERE r.tenant_id=UUID_TO_BIN(?) AND r.request_id=UUID_TO_BIN(?) AND r.status='reserved' ORDER BY r.created_at LIMIT 1 FOR UPDATE`, tenantID, auditID).Scan(&reservationID, &accountID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE budget_accounts SET reserved_microunits=reserved_microunits-?,settled_microunits=settled_microunits+?
WHERE id=UUID_TO_BIN(?) AND reserved_microunits>=?`, cost, cost, accountID, cost)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("budget reservation %s is smaller than settled cost", reservationID)
	}
	ledgerID, _ := uuid.NewV7()
	pricingVersion := ""
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(mp.semantic_version,'') FROM audits a JOIN measurement_protocols mp ON mp.id=a.protocol_id WHERE a.id=UUID_TO_BIN(?)`, auditID).Scan(&pricingVersion)
	_, err = tx.ExecContext(ctx, `INSERT INTO cost_ledger (id,tenant_id,budget_account_id,request_id,entry_type,amount_microunits,currency,pricing_version,created_at)
SELECT UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'settlement',?,currency,?,UTC_TIMESTAMP(6) FROM budget_accounts WHERE id=UUID_TO_BIN(?)`, ledgerID.String(), tenantID, accountID, auditID, cost, pricingVersion, accountID)
	if err != nil {
		return fmt.Errorf("record sample cost: %w", err)
	}
	return nil
}

// releaseAuditReservation returns any unused reservation once the audit is terminal.
func releaseAuditReservation(ctx context.Context, tx *sql.Tx, tenantID, auditID string) error {
	var reservationID, accountID string
	var amount int64
	err := tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(budget_account_id),amount_microunits
FROM budget_reservations WHERE tenant_id=UUID_TO_BIN(?) AND request_id=UUID_TO_BIN(?) AND status='reserved' ORDER BY created_at LIMIT 1 FOR UPDATE`, tenantID, auditID).Scan(&reservationID, &accountID, &amount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var settled int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_microunits),0) FROM cost_ledger WHERE budget_account_id=UUID_TO_BIN(?) AND request_id=UUID_TO_BIN(?) AND entry_type='settlement'`, accountID, auditID).Scan(&settled); err != nil {
		return err
	}
	remaining := amount - settled
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE budget_accounts SET reserved_microunits=IF(reserved_microunits>=?,reserved_microunits-?,0) WHERE id=UUID_TO_BIN(?)`, remaining, remaining, accountID); err != nil {
			return err
		}
		ledgerID, _ := uuid.NewV7()
		if _, err := tx.ExecContext(ctx, `INSERT INTO cost_ledger (id,tenant_id,budget_account_id,request_id,entry_type,amount_microunits,currency,pricing_version,created_at)
SELECT UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'release',?,currency,'',UTC_TIMESTAMP(6) FROM budget_accounts WHERE id=UUID_TO_BIN(?)`, ledgerID.String(), tenantID, accountID, auditID, -remaining, accountID); err != nil {
			return err
		}
	}
	status := "released"
	if settled > 0 {
		status = "settled"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE budget_reservations SET status=? WHERE id=UUID_TO_BIN(?)`, status, reservationID); err != nil {
		return err
	}
	return nil
}
