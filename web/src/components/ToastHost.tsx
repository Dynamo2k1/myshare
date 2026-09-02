import { toasts, dismiss } from "../lib/toast";

export function ToastHost() {
  return (
    <div class="toast-host">
      {toasts.value.map((t) => (
        <div key={t.id} class={`toast toast-${t.kind}`} role="status">
          <span class="toast-msg">{t.message}</span>
          {t.action && (
            <button
              class="toast-action"
              onClick={() => {
                t.action!.run();
                dismiss(t.id);
              }}
            >
              {t.action.label}
            </button>
          )}
          <button class="toast-x" onClick={() => dismiss(t.id)} aria-label="Dismiss">
            ×
          </button>
        </div>
      ))}
    </div>
  );
}
