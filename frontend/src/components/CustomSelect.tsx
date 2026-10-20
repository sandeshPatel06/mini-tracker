import { useEffect, useId, useRef, useState } from 'react';
import type { CSSProperties, KeyboardEvent, ReactNode } from 'react';
import { Icon } from './Icon';

export interface SelectOption {
  value: string;
  label: string;
  triggerLabel?: string;
  leading?: ReactNode;
  meta?: string;
  disabled?: boolean;
}

interface CustomSelectProps {
  value: string;
  options: SelectOption[];
  onChange: (value: string) => void;
  ariaLabel: string;
  id?: string;
  className?: string;
  align?: 'left' | 'right';
  compact?: boolean;
  fullWidth?: boolean;
  disabled?: boolean;
  triggerStyle?: CSSProperties;
}

export function CustomSelect({
  value,
  options,
  onChange,
  ariaLabel,
  id,
  className = '',
  align = 'left',
  compact = false,
  fullWidth = false,
  disabled = false,
  triggerStyle,
}: CustomSelectProps) {
  const reactId = useId();
  const triggerId = id || `${reactId}-trigger`;
  const listboxId = `${reactId}-listbox`;
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const optionRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);

  const selectedIndex = options.findIndex((option) => option.value === value);
  const selected = selectedIndex >= 0 ? options[selectedIndex] : options[0];
  const activeOption = open ? options[activeIndex] : undefined;

  useEffect(() => {
    if (!open) return;

    const closeOnOutsideClick = (event: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    };
    const closeOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') {
        setOpen(false);
        triggerRef.current?.focus();
      }
    };

    document.addEventListener('mousedown', closeOnOutsideClick);
    document.addEventListener('keydown', closeOnEscape);
    return () => {
      document.removeEventListener('mousedown', closeOnOutsideClick);
      document.removeEventListener('keydown', closeOnEscape);
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    optionRefs.current[activeIndex]?.scrollIntoView({ block: 'nearest' });
  }, [activeIndex, open]);

  const openMenu = () => {
    const nextIndex = options.findIndex((option) => option.value === value);
    setActiveIndex(nextIndex >= 0 ? nextIndex : 0);
    setOpen(true);
  };

  const selectOption = (option: SelectOption) => {
    if (option.disabled) return;
    onChange(option.value);
    setOpen(false);
    triggerRef.current?.focus();
  };

  const moveActive = (delta: number) => {
    if (options.length === 0) return;
    setActiveIndex((current) => {
      const next = current + delta;
      if (next < 0) return options.length - 1;
      if (next >= options.length) return 0;
      return next;
    });
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (!open) {
        openMenu();
      } else {
        moveActive(event.key === 'ArrowDown' ? 1 : -1);
      }
      return;
    }
    if (event.key === 'Home' || event.key === 'End') {
      if (!open) return;
      event.preventDefault();
      setActiveIndex(event.key === 'Home' ? 0 : options.length - 1);
      return;
    }
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      if (!open) {
        openMenu();
      } else if (activeOption) {
        selectOption(activeOption);
      }
      return;
    }
    if (event.key === 'Escape' && open) {
      event.preventDefault();
      setOpen(false);
    }
    if (event.key === 'Tab') {
      setOpen(false);
    }
  };

  const classes = [
    'custom-select',
    fullWidth ? 'full-width' : '',
    compact ? 'compact' : '',
    className,
  ].filter(Boolean).join(' ');

  return (
    <div className={classes} ref={rootRef}>
      <button
        ref={triggerRef}
        id={triggerId}
        type="button"
        className="custom-select-trigger"
        style={triggerStyle}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listboxId : undefined}
        aria-activedescendant={activeOption ? `${reactId}-option-${activeIndex}` : undefined}
        aria-label={ariaLabel}
        disabled={disabled}
        onClick={() => {
          if (open) {
            setOpen(false);
          } else {
            openMenu();
          }
        }}
        onKeyDown={handleKeyDown}
      >
        {selected?.leading}
        <span className="custom-select-value">
          {selected ? selected.triggerLabel || selected.label : value}
        </span>
        <Icon name="chevron-down" size={compact ? 12 : 14} className="custom-select-chevron" />
      </button>

      {open && (
        <div
          id={listboxId}
          className={`custom-select-menu ${align === 'right' ? 'align-right' : ''}`.trim()}
          role="listbox"
          aria-label={ariaLabel}
        >
          {options.length === 0 ? (
            <div className="custom-select-empty">No options</div>
          ) : (
            options.map((option, index) => {
              const isSelected = option.value === value;
              return (
                <button
                  key={option.value}
                  ref={(element) => {
                    optionRefs.current[index] = element;
                  }}
                  id={`${reactId}-option-${index}`}
                  type="button"
                  role="option"
                  aria-selected={isSelected}
                  className={`custom-select-option ${index === activeIndex ? 'is-active' : ''}`.trim()}
                  disabled={option.disabled}
                  title={option.label}
                  tabIndex={-1}
                  onMouseDown={(event) => event.preventDefault()}
                  onMouseEnter={() => setActiveIndex(index)}
                  onClick={() => selectOption(option)}
                >
                  {option.leading && <span className="custom-select-leading">{option.leading}</span>}
                  <span className="custom-select-option-label">{option.label}</span>
                  {option.meta && <span className="custom-select-option-meta">{option.meta}</span>}
                </button>
              );
            })
          )}
        </div>
      )}
    </div>
  );
}
