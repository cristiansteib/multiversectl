---
name: multiversectl
description: Run, inspect and work in isolated copies ("universes") of a multi-repo project with multiversectl, so several agents or branches can run side by side. Use it when the task needs a running copy of the project, or when you see a multiverse project root or a multiverse.yaml.
---
<!-- multiversectl-skill-version: {{version}} -->

# multiversectl

Each **universe** is a numbered (or named) copy of the whole project: one git worktree per repository on its own branch, its own containers, volumes and hostnames. Work in a universe of your own and fix what is in your way there: that is the point of it.

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
6. Run things in it with `universe exec <universe> <app> -- <command>` or `universe run`. Each app is its own compose project, `<project>-u<N>-<app>`, so a plain `docker compose ps` in a worktree shows nothing; use `-p` with that name, or `exec`. For `docker logs` or `docker cp` take the container name from `containers`. An app with several services needs `--service` unless the manifest gives it a `defaultService`; if it does not, tell the user to add one.

## When something is wrong

Fixing it is part of the job. Run `universe doctor <universe>`, read the error (it says what to run), find the cause, fix it in your universe's repositories, and run it again to check. That includes the project's own files: a compose label, a missing example env, a bad script. Make it a normal commit on your branch, and say in your answer what you changed and why. The `multiverse.yaml` and the shared clones are not yours to edit: tell the user what should change there.

## Care

- `--json` for anything you parse. Messages and tables come as one JSON object per line; errors on stderr.
- You may `universe down` and `universe rm` the universes you created. Ask first for any other, and for `system prune`, `system proxy` and `reset`, which reach beyond one universe.
- Secrets (`.env`, keys): you may look at which keys exist, but do not print their values or commit them. If `up` says a secret file is missing, tell the user where it goes.
- The proxy and the web server are shared with other agents and with the user: diagnose them with `doctor` and say what to run; do not stop or restart them yourself.
- No force pushes, and no resets of a branch you did not start. Never touch another universe or anything under the project's `.multiverse/`.
