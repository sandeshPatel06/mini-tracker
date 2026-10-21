import React, { useState, useEffect } from 'react';
import { Modal } from './Modal';

interface TaskGoalModalProps {
  isOpen: boolean;
  onClose: () => void;
  currentTask: string;
  onSave: (task: string) => void;
}

export function TaskGoalModal({ isOpen, onClose, currentTask, onSave }: TaskGoalModalProps) {
  const [taskInput, setTaskInput] = useState(currentTask);

  useEffect(() => {
    if (isOpen) {
      setTaskInput(currentTask);
    }
  }, [isOpen, currentTask]);

  const wordCount = taskInput.trim().split(/\s+/).filter(Boolean).length;
  const isOverLimit = wordCount > 20;

  const handleSave = () => {
    onSave(taskInput.trim());
    onClose();
  };

  const handleClear = () => {
    onSave('');
    onClose();
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Set Target Work Task"
      maxWidth={460}
      footer={
        <>
          <button onClick={onClose} className="btn btn-secondary btn-sm">
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={isOverLimit}
            className="btn btn-primary btn-sm"
            style={{ opacity: isOverLimit ? 0.6 : undefined, cursor: isOverLimit ? 'not-allowed' : undefined }}
          >
            Save & Align AI Analytics
          </button>
        </>
      }
    >
      <p style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 14, lineHeight: 1.5 }}>
        Describe what you are currently working on. AI analytics will evaluate your screenshots, keypresses, and mouse activity specifically against this goal.
      </p>

      <textarea
        autoFocus
        rows={3}
        className="form-input"
        value={taskInput}
        onChange={(e) => setTaskInput(e.target.value)}
        placeholder="e.g. Fixing database migration bug and optimizing mouse event loop performance..."
        style={{
          resize: 'none',
          marginBottom: 8,
        }}
      />

      <div className="flex items-center justify-between mt-4" style={{ marginBottom: 4 }}>
        <span
          style={{
            fontSize: 11,
            color: isOverLimit ? 'var(--danger-text)' : 'var(--text-muted)',
          }}
        >
          Word count: {wordCount} / 20 words max
        </span>
        {currentTask && (
          <button
            onClick={handleClear}
            style={{
              background: 'none',
              border: 'none',
              color: 'var(--danger-text)',
              fontSize: 11,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            Clear Task
          </button>
        )}
      </div>
    </Modal>
  );
}
