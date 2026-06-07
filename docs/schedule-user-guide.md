# Schedule User Guide

Use `/schedule` to manage time-based task execution in a weekly calendar view.

## What This Page Is For

`/schedule` is for tasks that should run at a specific time or on a repeating cadence.

Why this matters:

- Keeps recurring automation predictable.
- Gives a visual weekly timeline of upcoming runs.
- Lets you quickly rebalance execution times with drag-and-drop.

## Weekly Calendar Navigation

At the top of `/schedule`:

- `Previous Week`
- `This Week`
- `Next Week`

Use these to move across calendar weeks.

Use this to plan load across days/hours instead of crowding everything into one time window.

## Create a Scheduled Task

1. Click `+ New Scheduled Task`.
2. Fill task fields (`Title`, `Prompt`, optional model, priority, tag).
3. Set schedule fields:
   - `Run At`
   - `Repeat` (`Once`, `Every N Seconds`, `Every N Minutes`, `Every N Hours`, `Daily`, `Weekly`, `Monthly`)
   - `Repeat Every` interval (shown when repeat is not `Once`)
4. Click `Create Scheduled Task`.

## Repeat Types

| Repeat Type | Use It For |
|---|---|
| Once | A single future run. |
| Seconds | High-frequency testing or development-only loops. |
| Minutes | Short operational intervals. |
| Hours | Repeated maintenance throughout a day. |
| Daily | Daily checks or recurring implementation tasks. |
| Weekly | Weekly cleanup, reporting, or backlog review. |
| Monthly | Long-running maintenance cadence. |

## Drag and Drop Rescheduling

You can drag scheduled items to another day/time slot.

- Drop updates the task's schedule time.
- Clicking a scheduled item opens task detail.

Only tasks with valid schedules can be dragged between time slots.

This is the fastest way to rebalance timing after priorities change.

## Visual Signals

- Scheduled task blocks are shown in the timeline grid.
- `Today` is highlighted.
- Current-time line indicators are shown in current-week view.

Use these signals to quickly spot what should run soon vs later.

## Editing Schedules in Task Detail

From `/tasks/{id}` -> `Schedules` tab:

- Add schedules
- Edit existing schedules
- Remove schedules

This is useful for precise per-task schedule maintenance.

## How Schedule Relates To Tasks

A schedule belongs to a task. The task defines the prompt, model, agent, attachments, repository context, and review behavior. The schedule controls only when and how often that task runs.

Use the task board for immediate work. Use Schedule when the timing matters — recurring automation, deferred execution, or a calendar-based maintenance cadence.

Scheduled runs enter the same worker queue as regular task runs. If worker slots are full, a scheduled run waits for a free slot just like any other task.
