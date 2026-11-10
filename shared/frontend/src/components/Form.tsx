import type { FormEvent, ReactNode } from "react";
import type { Fields } from "../api/client";
export function Form({
  busy,
  submit,
  collectFields,
  children,
  className = "form-stack integration-form",
  confirm,
  onSuccess,
}: {
  children: ReactNode;
  busy: boolean;
  submit: (fields: Fields) => Promise<boolean>;
  collectFields: (
    form: HTMLFormElement,
    submitter: HTMLElement | null,
  ) => Fields;
  className?: string;
  confirm?: string;
  onSuccess?: () => void;
}) {
  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || (confirm && !window.confirm(confirm))) return;
    const form = event.currentTarget;
    const fields = collectFields(
      form,
      (event.nativeEvent as SubmitEvent).submitter,
    );
    if (await submit(fields)) onSuccess?.();
  }
  return (
    <form onSubmit={onSubmit} className={className}>
      <fieldset className="form-fields" disabled={busy}>
        {children}
      </fieldset>
    </form>
  );
}
