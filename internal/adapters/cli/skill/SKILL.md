---
name: multiversectl
description: Run, inspect and work in isolated copies ("universes") of a multi-repo project with multiversectl, so several agents or branches can run side by side. Use it when the task needs a running copy of the multiverse, or when you see a multiverse multiverse root or a multiverse.yaml.
---
<!-- multiversectl-skill-version: {{version}} -->

# multiversectl

Each **universe** is a numbered (or named) copy of the whole multiverse: one git worktree per repository on its own branch, its own containers, volumes and hostnames. Work in a universe of your own and fix what is in your way there: that is the point of it.

## Discover

- `multiversectl --json` lists the commands; `multiversectl <command> --help --json` describes one (`--full` for every command at once).
- `multiversectl universe ls -o json,less` is the least you need: per universe its state, folder and `urls` (`<app>/<host>` to its address: `api/devapi` is not `api/api`, so you need not guess which one is the API; `<app>/<host>:forward` is the same host by IP and port, the one a phone or another client that refuses plain http to a hostname must use, present when the manifest lists the host in the app's `portForward`). `-o json` adds the folder of each repository, branches, what is behind, each universe's `note` and `pr`, and `containers` (`<app>/<service>` to the name of its running container). `-o json,wide` is everything.
- `multiversectl universe ls <universe> -o json` is one universe. Prefer it to the table.

## Work

1. Look first: `universe ls -o json,less`. The `note` and `pr` say what another universe is for; reuse an idle universe of yours before making another.
2. `universe new --note "<what you are doing>"` takes the next free number (or `universe new <name>`; `universe checkout <branch>` for an existing branch).
3. `universe up <universe>` starts it; it can take minutes. Then `universe ls <universe> -o json`: edit and commit inside `checkout.<repo>` (`<dir>/<repo>`, on branch `u<N>`), browse at `urls`; `universe doctor` asks each address and says which one does not answer.
4. Leave a trace for whoever looks next: keep the note current (`universe note <universe> "<short text>"`) and, once the work has a pull request, `universe note <universe> --pr <url>`.
5. Code in a universe does not follow `main`. If `behind` is set, or `up` says the code is out of date: `universe sync <universe>` (`--merge` or a rebase only if you know the branch is yours).
6. Run things in it with `universe exec <universe> <app> -- <command>` or `universe run`. Each app is its own compose project, `<multiverse>-u<N>-<app>`, so a plain `docker compose ps` in a worktree shows nothing; use `-p` with that name, or `exec`. For `docker logs` or `docker cp` take the container name from `containers`. An app with several services needs `--service` unless the manifest gives it a `defaultService`; if it does not, tell the user to add one.

## Catalogs

A **catalog** is an index file (`catalog.yaml`) in a git repository. The **root catalog** is the one this machine starts from; it lists **nested catalogs** and **multiverses** (the leaves, each a `multiverse.yaml`). `multiversectl catalog list --json` prints the whole tree (`name`, `path`, `kind`, `repo`, `installedAs`, `children`); `catalog list <path> --json` is one node. Run it after any change to confirm the result instead of guessing. A node's `path` is what `--at` takes (`/` is the root).

- **`catalog new`** makes a new multiverse (`--type multiverse`, the default when `--repo` is given) or a new catalog (`--type catalog <name>`), and lists it in the tree.
- **`catalog add <owner/repo | url | path>`** lists something that already exists (a repository that has a `catalog.yaml` or `multiverse.yaml`). Use `add` for what exists and `new` for what does not yet.
- Flags: `--at <path>` where in the tree (default: asked; with `--yes` the root); `--url <repo>` use an existing repository for the new multiverse; `--repo <url>` a repository to include, repeatable, `--repo name=<url>` to set its name; `--name` the multiverse's name; `--create owner/name` make its repository; `--provider github|gitlab` and `--host <host>` for where; `--same-repo` (a catalog kept as a file in its parent's repository); `--local` commit but do not push; `--no-install` do not start it on this machine afterwards; `--yes` accept every question (nothing asks, so pass every answer you need).

### Offline catalogs, and installing part of the tree

Each catalog repository gets 20 seconds (`MULTIVERSECTL_CATALOG_TIMEOUT=5s` changes it). One that does not answer, say behind a VPN that is down, shows as `unavailable` in `catalog list --json` (`unavailable` field), the rest of its host is skipped for that run, and everything else stays usable. Catalogs are read at the same time (8 at once), so a slow one does not hold the others. A repository fetched before is used from its last copy, with a warning, and its node carries `stale` (the reason) in `catalog list --json`; the web view shows each one's state live while it fetches. So an `unavailable` node is not an error to fix: tell the user which one and carry on with the others.

`multiversectl start <catalog-path> --install-only` installs every multiverse under that node and makes no universe; `--exclude <catalog-path>` (repeatable) leaves a multiverse or a whole branch out. Without `--install-only`, `start` runs one.

To leave a branch out for good on this machine (an unreachable VPN catalog, say), `multiversectl catalog ignore <catalog-path>`: it is not read or fetched, what is under it goes with it, and it is not offered for installing; `catalog unignore <catalog-path>` undoes it. It is a setting of the machine: no catalog repository changes. The web view's Catalog page has the same on each row. Its rows also install: **Install** on a multiverse, or **Install…** on a catalog (with a tick per multiverse under it) runs `start <catalog-path> --install-only` for you, and the dialog shows the command to copy. `multiversectl catalog manage` is the same on one keyboard-driven screen (install, uninstall, leave out, bring back, remove, add, update) for a person at a terminal; its review list names the command for each change, and an agent should use those commands, not the screen. It is a setting of the machine: no catalog repository changes. The web view's Catalog page has the same on each row.

### A multiverse in its own repository

1. Create the empty repository (below), and clone it.
2. Inside the clone: `multiversectl catalog new . --name <name> --repo <url> --repo <url> ... --url <its clone URL> --at <catalog path> --no-install --yes`. `.` is the directory that becomes the multiverse's repository.
3. Confirm: `multiversectl catalog list --json`.

It writes `multiverse.yaml` and `AGENTS.md`, commits them and **pushes to the multiverse's repository** (its current branch). It then adds the multiverse to the catalog `--at` names, which is **a commit pushed straight to the default branch of that catalog's repository**. It prints each push and the files it changed. With `--local` it commits and pushes nothing, and does not list it (there is no URL to list).

Names: a repository's name in the manifest is its repository name, lowercased, minus the multiverse's name and a dash (`nnnn-cli` in `nnnn` is `cli`). `--repo cli=<url>` says it instead. The names are printed and are what `universe` commands use.

### Self-hosted GitLab (or GitHub)

`--create group/name --provider gitlab --host gitlab.example.test` creates the repository through `glab` on that host, or pass the whole URL: `--create https://gitlab.example.test/group/name`. The same without `--create`: make the repository yourself, `GITLAB_HOST=gitlab.example.test glab repo create group/name --private` (`glab repo create` has no `-H`; the host comes from `GITLAB_HOST`), then `--url git@gitlab.example.test:group/name.git`. `glab auth login --hostname <host>` signs in.

### Before you create anything

Creating a repository or pushing to a catalog is visible to other people. Ask the user first unless they asked you to create it, and say which repositories and which catalog you will write to.

### What the manifest has, and what it lacks

The new `multiverse.yaml` only lists the repositories. `universe up` starts every repository that has a `docker-compose.yml` as an app of its own name, with the hosts its compose labels give; one without a compose file is only checked out. To say more (hosts and ports, secrets, optional services, compose files, profiles) add `spec.apps` to the manifest, in the multiverse's own repository: edit, commit, push, then `multiversectl catalog update`. Each app takes `repo`, `hosts` (`name`, `service`, `port`), `secrets`, `compose`, `profiles`; the full reference is https://multiversectl.dev/reference/manifest.html. The manifest is the user's to change: propose the edit unless asked to make it.

## Web view

`multiversectl server open` opens the web view in the browser, starting it when it is not running; a port taken by another process moves it to the next free one (the URL printed is the real one). `server status` says where it is.

## When something is wrong

Fixing it is part of the job. Run `universe doctor <universe>`, read the error (it says what to run), find the cause, fix it in your universe's repositories, and run it again to check. That includes the multiverse's own files: a compose label, a missing example env, a bad script. Make it a normal commit on your branch, and say in your answer what you changed and why. The `multiverse.yaml` and the shared clones are not yours to edit: tell the user what should change there.

## Care

- `--json` for anything you parse. Messages and tables come as one JSON object per line; errors on stderr.
- You may `universe down` and `universe rm` the universes you created. Ask first for any other, and for `system prune`, `system proxy` and `reset`, which reach beyond one universe.
- Secrets (`.env`, keys): you may look at which keys exist, but do not print their values or commit them. If `up` says a secret file is missing, tell the user where it goes.
- The proxy and the web server are shared with other agents and with the user: diagnose them with `doctor` and say what to run; do not stop or restart them yourself.
- No force pushes, and no resets of a branch you did not start. Never touch another universe or anything under the multiverse's `.multiverse/`.
