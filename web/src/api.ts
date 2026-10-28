// Thin client for the control plane's JSON API. Errors carry the plain-language
// message the server wrote, ready to show to a person.

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export type Platform = "ios" | "android" | "windows" | "macos" | "linux";
export type DisplayStatus = "waiting" | "connected" | "not_seen";

export interface AppState {
  setup_done: boolean;
  signed_in: boolean;
  network_name?: string;
  email?: string;
  two_step?: boolean;
}

export interface Device {
  id: string;
  person_id: string;
  person_name: string;
  name: string;
  platform: Platform | "other";
  kind: string;
  status: DisplayStatus;
  last_seen_at: string | null;
  added_at: string;
  refresh_due: boolean;
  agent?: { version: string; problem: string; reported_at: string | null };
  advanced?: {
    public_key: string;
    address: string;
    received_bytes: number;
    sent_bytes: number;
    last_address_seen: string;
  };
}

export interface Person {
  id: string;
  name: string;
  devices: Device[];
}

export type Attention =
  | { kind: "server_offline" }
  | { kind: "device_unseen_30_days"; device_id: string; device_name: string }
  | { kind: "refresh_due"; device_id: string; device_name: string }
  | { kind: "refresh_failed"; device_id: string; device_name: string }
  | { kind: "device_problem"; device_id: string; device_name: string; problem: string };

export interface Home {
  network: { name: string; server_address: string; server_address_kind: "name" | "ip" };
  server: { status: "none" | "waiting" | "online" | "offline"; name?: string };
  people: Person[];
  attention: Attention[];
}

export interface PersonSummary {
  id: string;
  name: string;
  device_count: number;
}

export interface ServerSummary {
  id: string;
  name: string;
  status: "waiting" | "online" | "offline";
}

export interface NewServer {
  id: string;
  name: string;
  install: { linux: string; docker: string; compose: string };
}

export interface AddedDevice {
  device: Device;
  config: string;
  qr_png_base64: string;
  filename: string;
  app_link: string;
}

export interface AddedAgentDevice {
  device: Device;
  token: string;
  expires_at: string;
  install: { command: string };
}

export interface ActivityItem {
  id: string;
  at: string;
  who: string;
  text: string;
}

export interface TotpBegin {
  secret: string;
  url: string;
  qr_png_base64: string;
}

let onSignedOut: (() => void) | null = null;
export function setSignedOutHandler(fn: () => void) {
  onSignedOut = fn;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-Wardline": "1" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(0, "");
  }
  const text = await res.text();
  const data = text ? JSON.parse(text) : {};
  if (!res.ok) {
    if (res.status === 401 && path !== "/api/session") onSignedOut?.();
    throw new ApiError(res.status, (data as { error?: string }).error ?? "");
  }
  return data as T;
}

export const api = {
  state: () => request<AppState>("GET", "/api/state"),
  setup: (b: { network_name: string; email: string; password: string }) => request<{ email: string }>("POST", "/api/setup", b),
  login: (b: { email: string; password: string; code?: string }) =>
    request<{ need_code?: boolean; email?: string }>("POST", "/api/session", b),
  logout: () => request<object>("DELETE", "/api/session"),
  totpBegin: () => request<TotpBegin>("POST", "/api/totp/begin"),
  totpConfirm: (code: string) => request<object>("POST", "/api/totp/confirm", { code }),
  setServerAddress: (address: string) =>
    request<{ server_address: string; server_address_kind: "name" | "ip" }>("PUT", "/api/network/server-address", { address }),
  network: () => request<{ name: string; server_address: string; server_address_kind: "name" | "ip" }>("GET", "/api/network"),
  servers: () => request<ServerSummary[]>("GET", "/api/servers"),
  createServer: (name?: string) => request<NewServer>("POST", "/api/servers", { name }),
  home: () => request<Home>("GET", "/api/home"),
  people: () => request<PersonSummary[]>("GET", "/api/people"),
  addPerson: (name: string) => request<PersonSummary>("POST", "/api/people", { name }),
  device: (id: string) => request<Device>("GET", `/api/devices/${id}`),
  addDevice: (b: { person_id: string; platform: Platform }) => request<AddedDevice>("POST", "/api/devices", b),
  addAgentDevice: (b: { person_id: string; platform: Platform }) => request<AddedAgentDevice>("POST", "/api/devices", { ...b, agent: true }),
  removeDevice: (id: string) => request<object>("DELETE", `/api/devices/${id}`),
  activity: (f: { person_id?: string; device_id?: string } = {}) => {
    const q = new URLSearchParams();
    if (f.person_id) q.set("person_id", f.person_id);
    if (f.device_id) q.set("device_id", f.device_id);
    const s = q.toString();
    return request<ActivityItem[]>("GET", `/api/activity${s ? `?${s}` : ""}`);
  },
};
