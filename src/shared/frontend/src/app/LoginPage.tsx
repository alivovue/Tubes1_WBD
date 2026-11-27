import { useEffect, useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { APIError } from "../api/client";
import { collectFormFields } from "../api/forms";
import { AeroScene } from "../components/AeroScene";
import { Icon } from "../components/Icon";
import { Button, Field, Notice } from "../components/primitives";
import { WorkspaceBrand } from "./WorkspaceShell";
import { useSession } from "./session";

export type LoginContent = {
  eyebrow: string;
  title: string;
  description: string;
  features: readonly string[];
  portal: string;
  icon: string;
  submitLabel: string;
  securityNote: string;
  people?: boolean;
};

export function LoginPage({
  home,
  content,
}: {
  home: string;
  content: LoginContent;
}) {
  const { session, update, request } = useSession();
  const navigate = useNavigate();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  const [challengeRevision, setChallengeRevision] = useState(0);
  const [challengeLoading, setChallengeLoading] = useState(false);
  useEffect(() => {
    if (session.CSRF) return;
    const controller = new AbortController();
    setChallengeLoading(true);
    setError("");
    request("/session", undefined, undefined, controller.signal)
      .then((result) => {
        if (!controller.signal.aborted) update(result);
      })
      .catch((problem: Error) => {
        if (!controller.signal.aborted) setError(problem.message);
      })
      .finally(() => {
        if (!controller.signal.aborted) setChallengeLoading(false);
      });
    return () => controller.abort();
  }, [session.CSRF, request, update, challengeRevision]);
  async function login(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || !session.CSRF) return;
    setBusy(true);
    setError("");
    try {
      await request(
        "/login",
        collectFormFields(event.currentTarget, null),
        session.CSRF,
      );
      const identity = await request("/session");
      update(identity);
      navigate(home, { replace: true });
    } catch (problem) {
      if (problem instanceof APIError && problem.result.CSRF)
        update(problem.result);
      setError(
        problem instanceof Error
          ? problem.message
          : "Login failed. Please retry.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="auth-shell">
      <section className="auth-showcase" aria-labelledby="showcase-title">
        <WorkspaceBrand home={home} />
        <div className="auth-showcase__copy">
          <p className="eyebrow">{content.eyebrow}</p>
          <h1 id="showcase-title">{content.title}</h1>
          <p>{content.description}</p>
          <ul className="feature-list">
            {content.features.map((feature) => (
              <li key={feature}>
                <span>
                  <Icon name="check" />
                </span>
                {feature}
              </li>
            ))}
          </ul>
        </div>
        <AeroScene people={content.people} />
        <p className="auth-showcase__footer">
          Schakker Technologies · Internal systems
        </p>
      </section>
      <section className="auth-panel" aria-labelledby="login-title">
        <div className="auth-card">
          <div className="auth-card__mobile-brand">
            <WorkspaceBrand home={home} />
          </div>
          <div className="auth-card__emblem" aria-hidden="true">
            <Icon name={content.icon} />
          </div>
          <p className="eyebrow">{content.portal}</p>
          <h2 id="login-title">Welcome back</h2>
          <p className="auth-card__lead">
            Masuk menggunakan akun internal Schakker Anda.
          </p>
          {error && <Notice error>{error}</Notice>}
          {!session.CSRF && error && (
            <Button
              disabled={challengeLoading}
              onClick={() => setChallengeRevision((value) => value + 1)}
            >
              Retry
            </Button>
          )}
          <form className="form-stack" onSubmit={login}>
            <Field label="Username">
              <input name="username" autoComplete="username" required />
            </Field>
            <Field label="Password">
              <span className="input-with-action">
                <input
                  name="password"
                  type={showPassword ? "text" : "password"}
                  autoComplete="current-password"
                  required
                />
                <button
                  type="button"
                  onClick={() => setShowPassword((value) => !value)}
                  aria-label={showPassword ? "Hide password" : "Show password"}
                >
                  {showPassword ? "Hide" : "Show"}
                </button>
              </span>
            </Field>
            <Button disabled={busy || !session.CSRF}>
              {busy ? "Signing in…" : content.submitLabel}
            </Button>
          </form>
          <p className="security-note">{content.securityNote}</p>
          <p className="auth-help">
            Kesulitan masuk? Hubungi Platform Admin Anda.
          </p>
        </div>
      </section>
    </main>
  );
}
