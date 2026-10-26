---
name: multiversectl
description: Run, inspect and work in isolated copies ("universes") of a multi-repo project with multiversectl, so several agents or branches can run side by side. Use it when the task needs a running copy of the project, or when you see a multiverse project root or a multiverse.yaml.
---
<!-- multiversectl-skill-version: {{version}} -->

# multiversectl

Each **universe** is a numbered (or named) copy of the whole project: one git worktree per repository on its own branch, its own containers, volumes and hostnames. Work in a universe of your own; never in the shared clones or in another universe.

## Discover

- `multiversectl --json` lists the commands; `multiversectl <command> --help --json` describes one (`--full` for every command at once).
- `multiversectl universe ls -o json,less` is the least you need: per universe its state, folder and URLs. `-o json` adds the folder of each repository, branches and what is behind; `-o json,wide` is everything.
- `multiversectl universe ls <universe> -o json` is one universe. Prefer it to the table.

## Work

1. Look first: `universe ls -o json,less`. Reuse an idle universe of yours before making another.
2. `universe new` takes the next free number (or `universe new <name>`; `universe checkout <branch>` for an existing branch).
3. `universe up <universe>` starts it; it can take minutes. Then `universe ls <universe> -o json`: edit and commit only inside `checkout.<repo>` (`<dir>/<repo>`, on branch `u<N>`), browse at `urls`.
4. Code in a universe does not follow `main`. If `behind` is set, or `up` says the code is out of date: `universe sync <universe>` (`--merge` only if asked).
5. Run things in it with `universe exec <universe> <app> -- <command>` or `universe run`. Do not run `docker compose` in a worktree: each app is its own compose project, `<project>-u<N>-<app>`, so a plain `docker compose ps` there shows nothing.
6. When something is wrong: `universe doctor <universe>`, and read the error; it says what to run.

## Rules

- `--json` for anything you parse. Messages and tables come as one JSON object per line; errors on stderr.
- Never `universe rm`, `prune`, `reset` or `cleanup` without the user's explicit approval for that universe.
- Never read, print or commit `.env` files or other secrets. If `up` says a secret file is missing, tell the user where it goes.
- Several agents share the machine: touch only your own universe, and leave the proxy and the web server alone.
- No force pushes, no resets of a branch you did not start. Do not edit anything under the project's `.multiverse/` or the shared clones; use the CLI.
- Do not change `multiverse.yaml` or the compose files to make a universe start without telling the user why.
