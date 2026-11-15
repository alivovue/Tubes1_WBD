export type Fields = Record<string, string | string[]>;
export type APIResponse = { error?: string; CSRF?: string; location?: string };
export class APIError extends Error {
  constructor(
    public status: number,
    public result: APIResponse,
  ) {
    super(
      result.error || "The operation is temporarily unavailable. Please retry.",
    );
  }
}

export async function requestJSON<T extends APIResponse>(
  path: string,
  fields?: Fields,
  csrf?: string,
  signal?: AbortSignal,
): Promise<T> {
  return requestResource<T>(path, {
    method: fields ? "POST" : "GET",
    body: fields,
    csrf,
    signal,
  });
}

// Resource APIs use typed JSON bodies, including booleans and nested batch settings.
export async function requestResource<T extends APIResponse>(
  path: string,
  options: {
    method?: "GET" | "POST" | "PATCH";
    body?: unknown;
    csrf?: string;
    signal?: AbortSignal;
    idempotencyKey?: string;
  } = {},
): Promise<T> {
  const mutation = options.method === "POST" || options.method === "PATCH";
  const response = await fetch(path, {
    method: options.method || "GET",
    credentials: "same-origin",
    headers: {
      Accept: "application/json",
      ...(mutation
        ? {
            "Content-Type": "application/json",
            "X-CSRF-Token": options.csrf || "",
          }
        : {}),
      ...(options.idempotencyKey
        ? { "Idempotency-Key": options.idempotencyKey }
        : {}),
    },
    body:
      mutation && options.body !== undefined
        ? JSON.stringify(options.body)
        : undefined,
    signal: options.signal,
  });
  let result: T;
  try {
    result = await response.json();
    if (!result || typeof result !== "object" || Array.isArray(result))
      throw new Error("Invalid JSON response");
  } catch {
    throw new APIError(response.ok ? 502 : response.status, {
      error: "The server returned an unreadable response. Please retry.",
    });
  }
  if (!response.ok) throw new APIError(response.status, result);
  return result;
}
