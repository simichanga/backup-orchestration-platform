# Trying out BOP

This is the "just show me it working" guide. If you want the full
technical picture, that's in [README.md](README.md) and the [docs/](docs)
folder - this page is only about getting something running on your own
machine so you can click around.

## What is this thing?

BOP backs up databases and files on other machines. You tell it what
hosts you have and what to back up (an `inventory.yaml` file), and it
connects over SSH, runs the backup, and stores the result using
[restic](https://restic.net/) (a well-established backup tool BOP drives
under the hood - it doesn't reinvent that part). It can do this on a
schedule, keep old backups around for a while and delete the rest
(retention), and check that what it stored is actually restorable.

There's also a web dashboard now - a page you open in your browser that
shows you what hosts are configured, what backup jobs have run, and lets
you kick off a backup by clicking a button instead of typing a command.

## The fastest way to see it work (~2 minutes)

You need [Docker Desktop](https://www.docker.com/products/docker-desktop/)
running, and [Go](https://go.dev/dl/) and [restic](https://restic.net/)
installed. If you've been working in this repo already, you have these.

Open PowerShell in this folder and run:

```powershell
.\scripts\try-it-out.ps1
```

This script does everything for you:

1. Builds BOP.
2. Creates a small throwaway "pretend server" using Docker - just a
   folder with one text file in it, standing in for a real machine you'd
   back up.
3. Starts a real `bop controller` pointed at that pretend server.
4. Opens your browser to the dashboard.

You'll see two tokens printed in the terminal - paste either one into
the "API token" box on the page that opens:

- `demo-read-token` - lets you look around, but not trigger anything.
- `demo-write-token` - lets you look around *and* click "Trigger backup".

Paste `demo-write-token`, click **Connect**, then click **Trigger
backup** and pick the one host/plugin available. Within a couple of
seconds you'll see a real backup job go from queued to succeeded, a real
snapshot appear, and a real event timeline showing what happened at each
step (that "seal" ring icon fills in based on what BOP actually verified,
not just because the job finished).

When you're done, close the browser tab and press **Ctrl+C** in the
terminal to stop the controller. Then run:

```powershell
.\scripts\try-it-out.ps1 -Cleanup
```

to remove the Docker container it created. Nothing else on your machine
is touched - everything the script writes lives under a temp folder that
`-Cleanup` also deletes.

## If something goes wrong

The script checks for the tools it needs (Go, Docker, restic, SSH key
tools) and tells you what's missing and how to get it. The most likely
real failure is **port 22 already in use** - BOP's SSH connections always
go to port 22, so if you already have an SSH server running on this
machine, stop it first, or the script will tell you and exit cleanly
rather than fail halfway through.

If you want to see what's actually happening under the hood while it
runs, the terminal window running `try-it-out.ps1` is a real, unmodified
`bop controller` process - its logs are the same JSON log lines it would
print in production.

## Looking at it without the demo data

If you just want to see the empty app - the connect screen, the layout,
no fake data - you don't need Docker or restic for that part. You can
build and run `bop controller` with an empty inventory and `api.enabled:
true`; see [docs/03-getting-started/quickstart.md](docs/03-getting-started/quickstart.md)
for how to write a minimal `config.yaml`/`inventory.yaml` by hand, and
[docs/05-operations.md](docs/05-operations.md#http-api-and-web-ui) for the
`api.*` block. The dashboard will just show zeros and empty tables until
a real job runs.

## What to actually poke at

- **Dashboard** - fleet-wide stats, recent jobs, recent events.
- **Hosts** - what's in the inventory (just the one demo host here).
- **Jobs** - every backup run, filterable by status. Click one to see its
  full event timeline and the verification seal.
- **Snapshots** - what's actually stored, per host.
- **Events** - the raw event feed BOP emits for everything it does.
- **Trigger backup** (top right) - runs a backup right now, outside the
  schedule.

Try disconnecting and reconnecting with `demo-read-token` instead of the
write token, then hit "Trigger backup" - you'll see it fail with a clear
message instead of silently doing nothing or logging you out, which is
exactly the behavior that's supposed to happen for a token that can look
but not touch.
