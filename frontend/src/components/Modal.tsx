import React, { useEffect, useId, useRef } from 'react';
import { Icon } from './Icon';

export interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  title?: React.ReactNode;
  children: React.ReactNode;
  footer?: React.ReactNode;
  maxWidth?: number | string;
  ariaLabel?: string;
  actions?: React.ReactNode;
  initialFocusRef?: React.RefObject<HTMLElement | null>;
  closeOnBackdropClick?: boolean;
}

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(', ');

const escapeStack: Array<() => void> = [];
let scrollLockCount = 0;
let savedBodyOverflow: string | null = null;

function getFocusable(container: HTMLElement): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
    (el) => !el.hasAttribute('disabled') && el.getClientRects().length > 0
  );
}

export function Modal({
  isOpen,
  onClose,
  title,
  children,
  footer,
  maxWidth = 480,
  ariaLabel,
  actions,
  initialFocusRef,
  closeOnBackdropClick = true,
}: ModalProps) {
  const titleId = useId();
  const cardRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const onCloseRef = useRef(onClose);
  const initialFocusRefProp = useRef(initialFocusRef);

  useEffect(() => {
    onCloseRef.current = onClose;
    initialFocusRefProp.current = initialFocusRef;
  });

  useEffect(() => {
    if (!isOpen) return;
    if (scrollLockCount === 0) {
      savedBodyOverflow = document.body.style.overflow;
      document.body.style.overflow = 'hidden';
    }
    scrollLockCount += 1;
    return () => {
      scrollLockCount -= 1;
      if (scrollLockCount === 0 && savedBodyOverflow !== null) {
        document.body.style.overflow = savedBodyOverflow;
        savedBodyOverflow = null;
      }
    };
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen) return;

    previousFocusRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;

    const entry = () => onCloseRef.current();
    escapeStack.push(entry);

    const focusFirst = () => {
      const card = cardRef.current;
      if (!card) return;
      const preferred = initialFocusRefProp.current?.current;
      if (preferred && card.contains(preferred)) {
        preferred.focus();
        return;
      }
      const focusables = getFocusable(card);
      if (focusables.length > 0) {
        focusables[0].focus();
      } else {
        card.focus();
      }
    };
    focusFirst();

    const handleKeyDown = (e: KeyboardEvent) => {
      if (escapeStack[escapeStack.length - 1] !== entry) return;

      if (e.key === 'Escape') {
        if (e.repeat) return;
        onCloseRef.current();
        return;
      }

      if (e.key !== 'Tab') return;
      const card = cardRef.current;
      if (!card) return;
      const focusables = getFocusable(card);
      if (focusables.length === 0) {
        e.preventDefault();
        card.focus();
        return;
      }
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const active = document.activeElement;
      const outside = !card.contains(active);
      if (e.shiftKey) {
        if (outside || active === card || active === first) {
          e.preventDefault();
          last.focus();
        }
      } else if (outside || active === last) {
        e.preventDefault();
        first.focus();
      }
    };

    window.addEventListener('keydown', handleKeyDown);

    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      const index = escapeStack.lastIndexOf(entry);
      if (index !== -1) escapeStack.splice(index, 1);
      const previous = previousFocusRef.current;
      previousFocusRef.current = null;
      if (previous && document.contains(previous) && typeof previous.focus === 'function') {
        previous.focus();
      }
    };
  }, [isOpen]);

  if (!isOpen) return null;

  const hasHeader = Boolean(title || actions);

  return (
    <div
      className="modal-backdrop"
      onClick={closeOnBackdropClick ? onClose : undefined}
    >
      <div
        ref={cardRef}
        className="modal-card"
        style={{ maxWidth, position: 'relative' }}
        role="dialog"
        aria-modal="true"
        aria-labelledby={title ? titleId : undefined}
        aria-label={title ? undefined : ariaLabel}
        tabIndex={-1}
        onClick={(e) => e.stopPropagation()}
      >
        {hasHeader ? (
          <div className="modal-header">
            {title && (
              <h3
                className="modal-title"
                id={titleId}
                title={typeof title === 'string' ? title : undefined}
                style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
              >
                {title}
              </h3>
            )}
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                marginLeft: 'auto',
                flexShrink: 0,
              }}
            >
              {actions}
              <button
                type="button"
                onClick={onClose}
                className="modal-close"
                aria-label="Close modal dialog"
              >
                <Icon name="x" size={16} />
              </button>
            </div>
          </div>
        ) : (
          <button
            type="button"
            onClick={onClose}
            className="modal-close"
            aria-label="Close modal dialog"
            style={{ position: 'absolute', top: 10, right: 10, zIndex: 1 }}
          >
            <Icon name="x" size={16} />
          </button>
        )}

        <div className="modal-body">{children}</div>

        {footer && <div className="modal-footer">{footer}</div>}
      </div>
    </div>
  );
}
