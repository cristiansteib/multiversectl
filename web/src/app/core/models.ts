// The JSON the server sends, as the server writes it (internal/adapters/server/views.go).
// Everything here is read-only data: the page draws it, it never edits it.

export interface LocView { platform: string; short: string; path: string; host?: string; ref?: string; url?: string }

export interface NodeView {
  name: string; path: string; kind: 'catalog' | 'multiverse'; repo?: string; ref?: string; file?: string;
  installedAs?: string; unavailable?: string; stale?: string; ignored?: boolean; loc?: LocView; within?: string; children?: NodeView[];
}
export interface CatalogView { root: NodeView; installed: number }
export interface CatalogProgress { repo: string; ref?: string; phase: 'fetching' | 'done' | 'stale' | 'unavailable'; detail?: string; ms?: number }

export interface ServiceView { name: string; state: string; detail?: string; published?: string }
export interface UrlView { host: string; service: string; url: string }
export interface AppView { key: string; repo: string; branch?: string; path: string; running: number; expected: number; onDemand?: boolean; urls?: UrlView[]; services?: ServiceView[] }
export interface CheckoutView { repo: string; branch?: string; path: string; app?: string; follows?: string; behind?: number; ahead?: number; dirty?: boolean }

export interface UniverseView {
  name: string; slug: string; multiverse: string; domain: string; dir: string; running: number;
  created?: string; lastStarted?: string; note?: string; pr?: string; branches?: Record<string, string>;
  behind?: number; apps: AppView[]; checkouts: CheckoutView[];
}
export interface FleetMultiverse { name: string; root: string; universes: UniverseView[]; error?: string; warnings?: string[] }
export interface FleetView { multiverses: FleetMultiverse[]; error?: string }

export interface DiffView { files: number; added: number; removed: number }
export interface ReviewView { number: number; mark: string; title: string; url: string; state: 'open' | 'draft' | 'merged' | 'closed'; author?: string; updated?: string }
export interface CodeRow {
  repo: string; branch?: string; follows?: string; behind?: number; ahead?: number; dirty?: boolean; work?: DiffView;
  base?: string; baseUnknown?: boolean; change?: DiffView; uncommitted?: DiffView; review?: ReviewView; reviewError?: string;
}
export interface CodeView { rows: CodeRow[]; fetched?: string }
export interface FileView { path: string; oldPath?: string; status: string; added: number; removed: number; binary?: boolean }
export interface CommitView { sha: string; author?: string; at?: string; subject: string; body?: string }
export interface DiffFilesView { base?: string; files: FileView[]; commits?: CommitView[] }
export interface PatchView { path: string; patch: string; truncated?: boolean }

export interface RepoView { key: string; clone?: string; loc?: LocView; url: string; dir: string; cloned: boolean; follows?: string; behind?: number; ahead?: number; dirty?: boolean }
export interface HostDefView {
  name: string; service: string; port: number; entrypoint: string;
  alsoMatch?: string[]; portForward?: string; scheme?: string;
  /** From the app's compose files, as a universe last resolved them, not from the manifest. */
  discovered?: boolean;
}
export interface AppDefView { key: string; repo: string; services?: string[]; optional?: string[]; hosts?: HostDefView[] }
export interface MultiverseView {
  name: string; root: string; manifest?: string; from?: string; source?: string; universesDir?: string;
  domain: string; expose: string; network: string; entrypoints: Record<string, number>;
  repos: RepoView[]; apps: AppDefView[]; env?: Record<string, string>;
}

export interface ConfigRecord { sha256: string; at: string; version?: string }
/** The manifest a universe uses, with the one it was last started with beside it. */
export interface ConfigView {
  path: string; from?: string; source?: string; sha256: string; bytes: number; yaml: string; problems: string[];
  startedWith?: ConfigRecord; snapshot?: string; snapshotProblems?: string[]; changed: boolean;
}

export interface CheckView { name: string; status: string; detail?: string }
export interface DoctorView { checks: CheckView[]; ok: boolean }

export interface CatalogStatus { source?: string; local?: boolean; noRemote?: boolean; unavailable?: string }
export interface MultiverseStatus {
  name: string; root: string; from?: string; source?: string; repoUrl?: string; ref?: string; notes?: string[];
  stale?: boolean; manifestEdited?: boolean; manifestDiverged?: boolean; manifestAhead?: number; manifestBehind?: number; advice?: string;
}
export interface StatusReport { catalog: CatalogStatus; multiverses: MultiverseStatus[]; next?: string }
export interface OverviewView { version: string; readOnly?: boolean; status: StatusReport; rootLoc?: LocView; sources?: Record<string, LocView> }

export interface UpdateNotice { current: string; latest: string; releases: number; fixes: number; more?: boolean; changes?: number; highlights?: string[]; url: string }

export interface VersionInfo { version: string; commit?: string; date?: string; url?: string }
/** One line of a universe's journal: what was done in it, when, and who says so. */
export interface Note { id: string; at: string; by?: string; text: string }

export interface ToolVersion { name: string; version?: string; path?: string; note?: string; ok: boolean }
export interface SystemView {
  server: VersionInfo; tools: ToolVersion[]; latest: { version?: string; url?: string; newer?: boolean; error?: string };
  links: { repo: string; releases: string; issues: string };
  format: string; listen?: string; started?: string; readOnly?: boolean;
}
export interface NextView { number: number; skipped?: string[] }
export interface BranchView { name: string; repos: string[] }

export interface ContainerView { app: string; service: string; name: string; status: string; running: boolean; ports?: string }
export interface StatView { name: string; cpu: number; memory: number; memUsage: string; mem?: number; memLimit?: number; netIO: string; pids: number }
export interface FleetStatRow extends StatView { multiverse: string; universe: string; app: string; service: string }
export interface FleetStatsView { at: string; rows: FleetStatRow[]; sources?: { multiverse: string; error: string }[] }

export type OpAction = 'up' | 'down' | 'rm' | 'sync' | 'restart' | 'new' | 'checkout' | 'update' | 'selfupdate' | 'install';
export interface OpRequest {
  action: OpAction; multiverse?: string; universe?: string; app?: string; service?: string; name?: string; note?: string;
  branch?: string; base?: string; onlyExisting?: boolean; confirm?: string; force?: boolean;
  /** install: the catalog path to set up, where one multiverse should live, and the paths under it to leave out. */
  path?: string; root?: string; exclude?: string[];
}
export interface PlanTarget { name: string; root: string; path?: string; skip?: string; renamed?: string }
export interface PlanView { targets: PlanTarget[] }
export interface OpLine { kind: 'info' | 'ok' | 'warn' | 'out' | 'err'; text: string }
export interface Op {
  id: string; action: string; title: string; multiverse: string; universe?: string;
  state: 'running' | 'done' | 'failed'; error?: string; started: string; ended?: string; lines: OpLine[];
}
export interface Hello { version: string; commit?: string; date?: string; readOnly?: boolean; ops: Op[] }
