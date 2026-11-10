import { useEffect, useState } from "react";
// Only navigation presentation is shared; authentication and routes remain local.
export function useNavigation(pathname: string, search: string) {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    setOpen(false);
  }, [pathname, search]);
  useEffect(() => {
    document.getElementById("main-content")?.focus();
  }, [pathname]);
  useEffect(() => {
    document.body.classList.toggle("sidebar-is-open", open);
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("keydown", close);
    return () => {
      document.body.classList.remove("sidebar-is-open");
      document.removeEventListener("keydown", close);
    };
  }, [open]);
  return { open, setOpen };
}
