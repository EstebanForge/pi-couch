# Couch Pi

A phone-first web client for the pi coding agents on this server. This glossary fixes the words the plan, code, and UI use.

## Language

**Session**:
One pi session: a single jsonl file (a tree of entries) under `~/.pi/agent/sessions/`, whatever process started it.
_Avoid_: conversation, chat, thread

**Workdir**:
The folder a Session's pi process runs in. Stored in the session file's `cwd`.
_Avoid_: workspace, project, cwd, repo

**Repo**:
A git repository at a Session's Workdir or at a direct subfolder of it.
_Avoid_: repository, workspace

**Changes**:
The working-tree git diff of one Repo.
_Avoid_: diff, dirty files

**Comment**:
A note bound to one Repo, file, and line, deliverable to a Session as input. A Comment has state: pending or sent.
_Avoid_: note, review, remark

**Steering**:
Submitting input to a Session while its agent is running. Pi queues it by its own steering rules.
_Avoid_: interrupt, injection

**Driver**:
The one live process allowed to submit input to a Session.
_Avoid_: owner, backend, controller

**Status**:
A live agent's detected state: working, idle, blocked, done, unknown. Same values herdr reports.
_Avoid_: state, health

**Stop**:
End a Session's live driver. The Session file survives on disk.
_Avoid_: kill, close, terminate

**Resume**:
Start a pi process on an existing Session file.
_Avoid_: continue, reopen, adopt

**Close**:
Hide a Session from the list in the UI. Never deletes.
_Avoid_: remove, delete

## Scope statements

pi sessions only. Android first. No guests. Couch Pi is the working name.

## External terms (read-only, at the integration boundary)

herdr's own words (its workspace, tab, pane, agent) mean what herdr means; they appear only where Couch Pi calls herdr's API and never enter this glossary.
