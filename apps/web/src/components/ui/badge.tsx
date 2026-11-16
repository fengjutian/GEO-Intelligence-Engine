import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '../../lib/utils'

const badgeVariants = cva('ui-badge', { variants: { variant: { default: 'ui-badge-default', secondary: 'ui-badge-secondary', outline: 'ui-badge-outline', warning: 'ui-badge-warning' } }, defaultVariants: { variant: 'default' } })

export function Badge({ className, variant, ...props }: React.ComponentProps<'span'> & VariantProps<typeof badgeVariants>) {
  return <span data-slot="badge" className={cn(badgeVariants({ variant }), className)} {...props} />
}
