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
  const response = await fetch(path, {
    method: fields ? "POST" : "GET",
    credentials: "same-origin",
    headers: fields
      ? { "Content-Type": "application/json", "X-CSRF-Token": csrf || "" }
      : { Accept: "application/json" },
    body: fields ? JSON.stringify(fields) : undefined,
    signal,
  });
  const result = (await response.json().catch(() => ({
    error: "The server returned an unreadable response. Please retry.",
  }))) as T;
  if (!response.ok) throw new APIError(response.status, result);
  return result;
}
