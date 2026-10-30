---
name: multiversectl
description: Run, inspect and work in isolated copies ("universes") of a system spread over several repositories with multiversectl, so several agents or branches can run side by side. Use it when the task needs a running copy of the multiverse, or when you see a multiverse root or a multiverse.yaml.
---
<!-- multiversectl-skill-version: {{version}} -->

# multiversectl

Each **universe** is a copy of the whole multiverse: one git worktree per repository on its own branch, its own containers, volumes and hostnames. Work in a universe of your own and fix what is in your way there: that is the point of it.

## Discover

- `multiversectl --json` lists the commands; `<command> --help --json` describes one (`--full` for every command at once). Read those instead of guessing a flag.
- `multiversectl schema [field path]` prints what a manifest takes, field by field, from this binary: `schema spec.apps`, `schema spec.apps.hosts.port`, `--kind catalog`. A field shown by name only is printed by its own path; `--depth` for more at once.
- `universe ls -o json,less` is the least you need: per universe its state, folder and `urls` (`<app>/<host>` to its address, so `api/devapi` is not guesswork; `<app>/<host>:forward` is the same host by IP and port, which a phone or any client refusing plain http to a hostname needs). `-o json` adds each repository's folder and branch, what is behind, the `note` and `pr`, and `containers` (`<app>/<service>` to the container's name). `-o json,wide` is everything. `universe ls <universe> -o json` is one universe: prefer it to the table.

## Work

1. Look first: `universe ls -o json,less`. The `note` and `pr` say what another universe is for; reuse an idle one of yours before making another.
2. Name it after the work: `universe new fix/login --note "<what you are doing>"` starts that branch in every repository, so the branch itself says what it is for. Keep to the convention the multiverse states — a rule in an `AGENTS.md`, or `spec.branch` in its `multiverse.yaml` (that one names universes by number, so a plain `universe new` follows it); with none, `<type>/<subject>` (`fix/login`, `feat/invoicing`) is enough. A nameless, numbered universe is for a try you will throw away, not for work that becomes a pull request. `universe checkout <branch>` takes a branch that already exists.
3. `universe up <universe>` starts it: minutes, not seconds. Then edit and commit in the `checkout.<repo>` paths from `universe ls <universe> -o json`, and browse at `urls`; `universe doctor` asks each address and says which one does not answer.
4. Leave a trace for whoever looks next: `universe note <universe> "<short text>"`, and `universe note <universe> --pr <url>` once the work has a pull request.
5. Code in a universe does not follow `main`. If `behind` is set, or `up` says the code is out of date: `universe sync <universe>` (`--merge` or a rebase only if you know the branch is yours).
6. Run things in it with `universe exec <universe> <app> -- <command>` or `universe run`. Each app is its own compose project, `<multiverse>-<universe>-<app>` (`acme-u3-api`, `acme-fix-login-api`), so a plain `docker compose ps` in a worktree shows nothing: use `-p` with that name, or `exec`. For `docker logs` or `docker cp` take the container's name from `containers`. An app with several services needs `--service` unless the manifest gives it a `defaultService`; if it does not, tell the user to add one.

`universe config <universe>` says which manifest a universe uses, whether it loads, and whether it was edited since the universe started (`--diff` shows what): a universe goes on running on the manifest it was started with. `universe hooks <universe>` lists the commands the manifest ties to a universe's life, and `universe hooks run <universe> <event> [app...]` runs an event's again — which is how to retry one that was warned about.

## Catalogs

A **catalog** (`catalog.yaml`) is an index of nested catalogs and of **multiverses**, the leaves, each a `multiverse.yaml` (`kind: Multiverse`). The **root catalog** is the one this machine starts from. `catalog list --json` prints the tree; a node's `path` is what `--at` takes, `/` being the root. Run it after any change instead of guessing the result.

- `catalog new` makes a multiverse (`--repo <url>`, repeatable) or a catalog (`--type catalog <name>`), and lists it. `catalog add <owner/repo | url | path>` lists one that already exists. New for what does not exist yet, add for what does.
- One repository can define several multiverses — every `.yaml` in it that says `kind: Multiverse`, wherever it is: `multiversectl start <name> <owner/repo>` picks one, and a nameless start errors with the names.
- `multiversectl start <catalog-path> --install-only` installs every multiverse under a node (`--exclude <catalog-path>`, repeatable, leaves one or a branch out); without it, `start` also runs one.
- A catalog repository that does not answer within 20 seconds (`MULTIVERSECTL_CATALOG_TIMEOUT`) is `unavailable` in `catalog list --json`, or `stale` when a copy from before is used. Everything else stays usable, so it is not an error to fix: say which node it is and carry on. `catalog ignore <catalog-path>` leaves one out of this machine for good (`unignore` undoes it) and changes no repository.
- `catalog manage` is the same work on one screen for a person at a terminal; an agent uses the commands its review list names.

### A multiverse in its own repository

1. Create the empty repository and clone it.
2. In the clone: `multiversectl catalog new . --name <name> --repo <url> --repo <url> ... --url <its clone URL> --at <catalog path> --no-install --yes`.
3. Confirm with `multiversectl catalog list --json`.

It writes `multiverse.yaml` and `AGENTS.md`, pushes them to the multiverse's repository, and pushes a commit to the **default branch of the catalog's repository**; `--local` does neither and lists nothing. A repository's name in the manifest is its repository name, lowercased, minus the multiverse's name and a dash (`nnnn-cli` in `nnnn` is `cli`); `--repo cli=<url>` says it instead. Those names are what the `universe` commands use. On a self-hosted host, `--create group/name --provider gitlab --host gitlab.example.test` creates the repository through `glab`, or make it yourself (`GITLAB_HOST=<host> glab repo create group/name --private`; there is no `-H`) and pass `--url`.

**Creating a repository or pushing to a catalog is visible to other people. Ask the user first unless they asked for it, and say which repositories and which catalog you will write to.**

A new manifest only lists the repositories: `universe up` starts each one that has a `docker-compose.yml` as an app of its own name, with the hosts its compose labels give, and only checks out the rest. To say more — hosts, ports, secrets, profiles, compose files — add `spec.apps` (`multiversectl schema spec.apps`) in the multiverse's own repository: edit, commit, push, then `multiversectl catalog update`. The manifest is the user's to change: propose the edit unless asked to make it.

## Web view

`server open` opens it in the browser, starting it when it is not running; a taken port moves it, so the URL it prints is the real one. `server status` says where it is.

## When something is wrong

Fixing it is part of the job. Run `universe doctor <universe>`, read the error — it says what to run — find the cause, fix it in your universe's repositories and run it again. That includes the multiverse's own files: a compose label, a missing example env, a bad script. Make it a normal commit on your branch and say what you changed and why. The `multiverse.yaml` and the shared clones are not yours to edit: tell the user what should change there.

## Care

- `--json` for anything you parse. Messages and tables come as one JSON object per line; errors on stderr.
- You may `universe down` and `universe rm` the universes you created. Ask first for any other, and for `system prune`, `system proxy` and `reset`, which reach beyond one universe.
- Secrets (`.env`, keys): you may look at which keys exist, but do not print their values or commit them. If `up` says a secret file is missing, tell the user where it goes.
- The proxy and the web server are shared with other agents and with the user: diagnose them with `doctor` and say what to run; do not stop or restart them yourself.
- No force pushes, and no resets of a branch you did not start. Never touch another universe or anything under the multiverse's `.multiverse/`.

Documentation: <https://multiversectl.dev/docs/>. A bug in the tool itself, once you are sure it is not the multiverse: <https://github.com/cristiansteib/multiversectl/issues>.
