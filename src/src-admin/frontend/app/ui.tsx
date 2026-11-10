import { createContext, useContext, type ReactNode } from "react";
import { Form as SharedForm } from "@schakker/ui";
export {
  Button,
  Check,
  Field,
  Heading,
  Hidden,
  Icon,
  Notice,
  Panel,
  Table,
  stamp,
  pretty,
  permission,
  initials,
  url,
} from "@schakker/ui";
import { formFields } from "./api";
import type { Fields, PageData } from "./types";
export const PageContext = createContext<{
  data: PageData;
  busy: boolean;
  submit: (fields: Fields) => Promise<boolean>;
  refresh: () => void;
} | null>(null);
export function usePage() {
  const value = useContext(PageContext);
  if (!value) throw new Error("Page context is unavailable");
  return value;
}
export function Form(props: {
  children: ReactNode;
  className?: string;
  confirm?: string;
  onSuccess?: () => void;
}) {
  const { busy, submit } = usePage();
  return (
    <SharedForm
      {...props}
      busy={busy}
      submit={submit}
      collectFields={formFields}
    />
  );
}
