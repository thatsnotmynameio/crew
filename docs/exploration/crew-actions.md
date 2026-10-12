# crew actions: the engine

A draft to evolve, from a conversation on 2026-10-12. It is not a brainstorm yet.
A new actions engine, built alongside crew: today's crew keeps working, and this design does not start from its rules, routes or labels.
The engine is simple and made for our case.

**Status:** exploration only.
This draft still goes through a brainstorm, then a plan, before any code is written, and either can change or drop anything here.
The libraries, tools and versions named below (pgx, sqlc, connect-rpc, the migration tool, docker compose, Postgres itself) are first candidates, not choices: the brainstorm reviews each one and its version, since a better option may exist.

## The pieces

| Piece | Role |
|---|---|
| YAML (`when`, `steps`, `image`, `commands`, `settings`) | the syntax |
| The workflow as the queue's unit | what goes to a runner together and shares a workspace |
| `depends_on` between workflows | the order within a build; without it, everything starts at once |
| The pipeline as a definition, the build as a run | a set of files defines, each event makes a build |
| A runner that asks for work | labels, capacity, outgoing connections only |
| Hooks | what the runner does around a workflow |
| A container per step | where commands run, with the workspace mounted |
| Named queues with a limit | how many workflows of a kind run at once, on any runner |
| Multi-repo | one engine and the same runners for several repositories |

## Hierarchy

```text
engine (knows several repositories)
└── repository
    └── build (one run, started by an event)
        └── workflows   ← the queue's unit; depends_on between them; each goes whole to one runner
            └── steps   ← run together on the runner, in the same workspace, each in its own container
```

The engine receives the event, decides which workflows match, holds the queue, hands a workflow to whoever asks, receives states, logs and the result, releases the dependents and closes the build.
It never runs a step.
Even with a single runner, local and in the same process, the boundary stays: the engine hands out, the runner executes.

## Outcome: success or failure

A step and a workflow end only in success or failure.
Reacting to a failure is an ordinary step or workflow, with `when: status: failure`.
There are no verdicts, routes or states of crew's own.

## The YAML

One file per workflow, in `.crew/workflows/`.

```yaml
# .crew/workflows/test.yaml
when:
  - event: pull_request

queue: developer             # how many at once: the queue
labels:
  os: linux                  # where it runs: the runner
depends_on: [build]          # another workflow of the same build

steps:
  - name: test
    image: golang:1.27
    commands:
      - go test -race ./...

  - name: notify
    image: crew/comment      # a plugin is an image; settings arrive as environment variables
    settings:
      message: "the tests failed"
    when:
      - status: failure
```

`queue` and `labels` are separate: `labels` says where a workflow runs and `queue` says how many run at once.
`queue` never picks a runner: that is what `labels` do.

## The runner

```yaml
# on the runner's machine, outside any repository
name: notebook
labels: { os: linux }
capacity: 4
backend: docker              # or local
hooks: ~/.config/crew/hooks/
```

The runner asks the engine for work, so it only opens outgoing connections.
The capacity is the runner's: how many workflows it runs at once.
More parallelism means more runners or more capacity.

A workflow's path through the runner:

1. The runner asks for work and receives a workflow.
2. `pre-workflow` (a runner hook) can refuse the workflow: it is the policy point.
3. `environment` exports secrets and identity.
4. `checkout` creates the workspace; a hook can replace it.
5. Each step starts the container of its `image`, with the workspace mounted, and runs its `commands`.
6. `post-workflow`, the artifacts, and the result goes to the engine.

Hooks come from three places: the runner, the repository and a plugin.
The runner's hooks run on the machine.

## Queues and the match

When a runner asks for work, the engine hands it the first workflow that:

1. has every workflow it depends on finished;
2. has `labels` that match the runner's;
3. has a free slot in its queue;
4. comes first in the order: priority, then arrival.

The slot is taken at hand-out and freed at the end, or when the runner disappears and the workflow goes back to the queue.
Priority applies only after the slot: a workflow stuck in a full queue must not hold up one that has a slot in another queue.
A runner can serve several queues, so the order holds across every candidate it can take.

## Multi-repo

The engine is a service that knows several repositories, and the runners serve all of them.

```yaml
# the engine's configuration, outside any repository
repos:
  org/b: { priority: 10 }
  org/a: { priority: 0 }
queues:
  developer: 2
  clerk: 1
```

Queues live here, not in a repository: `developer: 2` holds for A and B at once.
Workflows only name their queue.

Priority is per project: with a workflow from A and one from B waiting in the same queue, B's goes first.
It is order, not dependency: neither workflow knows about the other.

## Database

Postgres from the start, and Postgres only.
Locally, a docker compose file in the repository starts it; the engine reads the database's URL from an environment variable, and as a service the same variable points at the production database.

Supporting several SQL dialects costs more than it gives: generated DDL per dialect, triggers to fake what one dialect lacks, or an ORM that hides the differences and the features.
One dialect lets the engine use what Postgres does well:

- **The database is the queue.** A workflow is claimed with `SELECT … FOR UPDATE SKIP LOCKED`, so a restart loses nothing and two engines never take the same row. A queue held in memory, with the database only as a backup, loses the running work on a restart.
- **A queue's limit is checked in the transaction that claims the workflow,** so "a slot is free" and "I took the slot" cannot race.
- **A lease column.** A runner renews its lease while it works; when a lease expires, the workflow goes back to the queue. Without a lease, a runner that dies leaves its workflow running forever.
- **A version column** for optimistic locking at accept: when two runners race for one workflow, the loser drops it.
- **LISTEN/NOTIFY** wakes the engine when something changes, instead of scanning every pending row on a timer.
- **Advisory locks** around each of the engine's loops, so several engines can run side by side, all alike, with no leader election.
- **Migrations** are plain `.sql` files embedded in the binary, run under an advisory lock at startup.

Tests of the store run against a real Postgres: the suite migrates a template database once, and each test creates its own database from the template (`CREATE DATABASE … TEMPLATE`), which is fast and parallel-safe.
The tests read the database's URL from the environment: locally from the compose file, in CI from a Postgres service container.

## Local and service

The local mode is the service with everything in one process, not a separate program.
Each thing that differs between the two is an interface from the first day:

| Seam | Local | Service |
|---|---|---|
| Database | Postgres from docker compose | a managed Postgres |
| Engine and runner | one process | the network, runners on other machines |
| Where events come from | polling, a manual start | webhooks: a service has a public URL |
| Identity | the machine's user and its `gh` token | runner registration tokens, API auth, per-repository credentials |
| Secrets | environment variables | stored encrypted in the database |
| Logs and artifacts | disk | disk or object storage |

Three things cost little now and a lot later:

1. **An API for clients (CLI, TUI, web), separate from the runner protocol.** Clients talk to the engine through it even in one process; otherwise a TUI that reads the engine's memory cannot work against a remote engine.
2. **Organization and repository on every row from the first migration.**
3. **The engine's configuration behind an interface:** a file locally, the database or an API as a service.

## Code architecture

A tree of its own, `internal/actions/`, in crew's module and binary (`crew actions engine`, `crew actions runner`).

```text
internal/actions/
  spec/                    # YAML: parse, lint, compile into a neutral form
  spec/dag/                # the depends_on graph, with cycle detection
  model/                   # Build, Workflow, Step; states as types, not strings
  scheduler/               # pure: from a snapshot of the candidates, picks one
  engine/                  # the use cases: create a build, hand out, accept, renew a lease, finish, release dependents
  store/postgres/          # pgx, embedded migrations, SKIP LOCKED, LISTEN/NOTIFY, advisory locks
  protocol/                # the engine–runner contract: protos and one Go interface
  transport/inproc/        # engine and runner in one process
  transport/connect/       # engine and runner on different machines
  api/                     # the API for the CLI, the TUI and the web, apart from the runner protocol
  source/github/           # where events come from
  runner/                  # the poller and its capacity, the lease, cancellation, the reporter, logs
  runner/executor/         # the phases and hooks of one workflow
  runner/backend/          # the Backend interface: local, docker, dummy
```

### What each part does, and why

- **`spec/`** compiles the YAML into a neutral form before anything runs. The engine validates a whole build at once, and the runner receives something already resolved. `spec/dag/` rejects a cycle with a clear error when the files are read.
- **`model/`** types every state and outcome. A result kept as a string, or a magic image name that means "run on the host", spreads checks through the code.
- **`scheduler/`** is pure: from a read-only snapshot of the candidate workflows and the asking runner, it returns a choice, with no I/O. The match is a chain of small stages: filters (dependencies finished, labels, a free slot) and then a ranking (priority, then arrival). Each stage is tested alone, and stages can be reordered.
- **`engine/`** holds the use cases and owns the transactions: it loads the snapshot, asks the scheduler, and writes the choice in the same transaction. A workflow is born `waiting` when it has dependencies and `pending` when it has none. When it finishes, the engine moves the dependents whose dependencies are all finished to `pending`, and skips the ones a failure rules out. A workflow is released as soon as its own dependencies finish, never at a stage boundary, so one slow workflow does not hold up unrelated ones. A workflow that waits for a slot is re-queued when a slot frees, instead of the queue being rescanned on a timer.
- **`store/postgres/`** implements one store interface per aggregate (builds, workflows, runners, queues), each declared where the engine uses it. A single store interface for everything, or one flat package of queries, grows past what anyone can read.
- **`protocol/`** is the only thing the engine and the runner share: the messages, defined in protos, and one Go interface that both the transports and the tests implement. The runner always opens the connection. The calls are:
  - register and declare: the runner's name, labels and capacity;
  - next: a long poll that returns a workflow whose labels match;
  - accept, guarded by the version column;
  - update: step states, with the lease renewed; the response carries a cancellation when there is one, so no separate channel is needed;
  - log: by offset, acknowledged, with a final "no more", so a resend is safe;
  - done.
- **`transport/inproc/`** implements the protocol in memory, for the local mode and the end-to-end tests. **`transport/connect/`** implements it over HTTP with connect-rpc, which crosses proxies and keeps the protos as the contract's documentation.
- **`api/`** serves clients, apart from the runner protocol.
- **`source/github/`** turns what happens on GitHub into events. Other sources (cron, a manual start) are adapters of their own.
- **`runner/`** runs one goroutine per unit of capacity, each looping on next and then running what it got. It renews the lease, stops a workflow the engine cancels, and reports step states and logs. Logs pass through a chain of `io.Writer`s (secret redaction, timestamps, chunks), so the executor never knows about the transport.
- **`runner/executor/`** runs one workflow through fixed phases (policy, environment, checkout, steps, artifacts, exit), with hooks from the runner, the repository and a plugin. Pre-hooks run in that order and post-hooks in reverse, and the first `checkout` hook found replaces the default checkout. The runner hands the executor a typed spec, not a long list of environment variables.
- **`runner/backend/`** is the interface for running a step: set up, run, destroy. `local` runs on the machine and `docker` in a container; `dummy` fakes steps from their settings (exit code, delay), so end-to-end tests need no docker.

### Boundaries

Imports point inward, checked by `depguard` as crew's other layers are:

- `spec`, `model` and `scheduler` import nothing with I/O.
- `runner` imports `protocol`, never `engine`, `scheduler` or `store`.
- `engine` imports no transport, backend or source.
- Nothing under `internal/actions/` imports today's crew (`internal/crew`, `core`, `engine`, `config`), and today's crew imports nothing under it. The one exception is `internal/proc`, the only way crew starts a child process.

### Day one

`spec`, `spec/dag`, `model`, `scheduler`, `engine`, `store/postgres`, `protocol`, `transport/inproc`, `runner`, `runner/executor`, `runner/backend/local` and `runner/backend/dummy`.
`transport/connect`, `runner/backend/docker`, `api` and `source/github` come when something uses them.

### Testing

- `scheduler` and `spec`: table tests, with no database and no clock.
- `store/postgres`: against a real Postgres, one database per test from a migrated template.
- `runner`: against a fake engine that serves the real protocol, not a mocked client, so requests really round-trip.
- End to end: a real engine and runner in one process, over `transport/inproc`, with the `dummy` backend: routing, depends_on, queue limits, cancellation, an expired lease, a restart.

## Set aside

- **The step as the unit.** Each step would go to the queue on its own and could land on another runner, with no shared disk. A session and the scripts that check it need the same workspace, and the checkout would repeat at every step. A step that should be scheduled on its own goes in a workflow of its own.
- **A queue that is only a runner tag.** Such a tag picks a pool of runners but limits nothing. The queue is a policy of whoever writes the workflows, and the capacity is a resource of whoever runs the runner: they are two limits.
- **A wait running on a runner.** A step that only waits for another build holds a slot without working. A wait, if there is one, lives in the engine and holds no runner.

## Later

- The kind of priority: strict, weighted or aging; and whether a workflow can set its own `priority` (only among workflows of the same repository).
- Dependencies across repositories: a workflow in A that waits for one in B, which belongs to another build.
- Where the repository's hooks run: on the machine, or inside the container, so they do not break the isolation.
- The plugin model: a container image that its `settings` configure, as environment variables, or a bundle of hooks that runs at the runner's phases and can replace the checkout or a step's commands.
- What `when: status` looks at within a workflow, and between workflows.
- What happens when a second event arrives for something already running: queue it, cancel the earlier one, or ignore it.
- Where events come from: webhooks, polling for events, cron, a manual start.
- The database tests on macOS: GitHub's macOS runners have no Docker, so CI's Postgres service container only exists on Linux. Skip them there, or install Postgres in the macOS job.
- Each library, tool and version named here (pgx, sqlc or hand-written SQL, connect-rpc, the migration tool, the Postgres version): the brainstorm reviews them.
- Before copying any code from another project, check its license: some open projects keep parts under non-commercial terms.

## Next: a build's lifecycle

- How an event enters the engine.
- How a build and its workflows are born: reading the files, applying the `when`s, linking the `depends_on`s.
- A workflow's states, from created to success or failure: waiting for dependencies, waiting for a slot, waiting for a runner, handed out, running, finished.
- What happens to the dependents of a workflow that failed.
- What the engine keeps to survive a restart. With the queue in Postgres, the builds, the queue, the slots and the leases are already rows; what is left is how an engine that starts resumes its loops and reaps the expired leases.
