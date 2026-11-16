import * as React from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import { cn } from '../../lib/utils'

export const Dialog = DialogPrimitive.Root
export const DialogTrigger = DialogPrimitive.Trigger
export const DialogClose = DialogPrimitive.Close
export const DialogPortal = DialogPrimitive.Portal

export function DialogOverlay({ className, ...props }: React.ComponentProps<typeof DialogPrimitive.Overlay>) {
  return <DialogPrimitive.Overlay data-slot="dialog-overlay" className={cn('ui-dialog-overlay', className)} {...props} />
}
export function DialogContent({ className, children, showClose, showCloseButton = showClose ?? true, ...props }: React.ComponentProps<typeof DialogPrimitive.Content> & { showClose?: boolean; showCloseButton?: boolean }) {
  return <DialogPortal><DialogOverlay /><DialogPrimitive.Content data-slot="dialog-content" className={cn('ui-dialog-content', className)} {...props}>{children}{showCloseButton && <DialogPrimitive.Close className="ui-dialog-close" aria-label="关闭"><X /></DialogPrimitive.Close>}</DialogPrimitive.Content></DialogPortal>
}
export function DialogHeader({ className, ...props }: React.ComponentProps<'div'>) { return <div data-slot="dialog-header" className={cn('ui-dialog-header', className)} {...props} /> }
export function DialogFooter({ className, ...props }: React.ComponentProps<'div'>) { return <div data-slot="dialog-footer" className={cn('ui-dialog-footer', className)} {...props} /> }
export function DialogTitle({ className, ...props }: React.ComponentProps<typeof DialogPrimitive.Title>) { return <DialogPrimitive.Title data-slot="dialog-title" className={cn('ui-dialog-title', className)} {...props} /> }
export function DialogDescription({ className, ...props }: React.ComponentProps<typeof DialogPrimitive.Description>) { return <DialogPrimitive.Description data-slot="dialog-description" className={cn('ui-dialog-description', className)} {...props} /> }
