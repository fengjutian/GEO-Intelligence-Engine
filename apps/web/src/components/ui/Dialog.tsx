import * as DialogPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import { ComponentPropsWithoutRef, ComponentRef, forwardRef } from 'react'

export const Dialog = DialogPrimitive.Root
export const DialogTrigger = DialogPrimitive.Trigger
export const DialogClose = DialogPrimitive.Close

type DialogContentProps = ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & {
  showClose?: boolean
}

export const DialogContent = forwardRef<ComponentRef<typeof DialogPrimitive.Content>, DialogContentProps>(
  ({ children, className = '', showClose = true, ...props }, ref) => (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="ui-dialog-overlay" />
      <DialogPrimitive.Content ref={ref} className={`ui-dialog-content ${className}`.trim()} {...props}>
        {children}
        {showClose && (
          <DialogPrimitive.Close className="ui-dialog-close" aria-label="关闭">
            <X aria-hidden="true" />
          </DialogPrimitive.Close>
        )}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  ),
)
DialogContent.displayName = DialogPrimitive.Content.displayName

export const DialogHeader = ({ className = '', ...props }: ComponentPropsWithoutRef<'div'>) => (
  <div className={`ui-dialog-header ${className}`.trim()} {...props} />
)

export const DialogFooter = ({ className = '', ...props }: ComponentPropsWithoutRef<'div'>) => (
  <div className={`ui-dialog-footer ${className}`.trim()} {...props} />
)

export const DialogTitle = DialogPrimitive.Title
export const DialogDescription = DialogPrimitive.Description
