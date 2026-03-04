---
name: todo
description: Manage todo lists and tasks using the memory system — add, complete, list, prioritize tasks. Use when user says 待办 todo task 任务 to-do 事项 checklist 清单 做完 完成了 done.
---

# Todo / Task Management

PicoClaw doesn't have a dedicated todo tool — use the **memory system** with consistent tags to simulate task management.

## Convention

All todo items use these tags:
- `#todo` — active task
- `#done` — completed task
- Priority tags: `#p1` (urgent), `#p2` (normal), `#p3` (low)
- Optional context tags: `#work`, `#personal`, `#project-name`

## Adding a task

```
/memory add Buy groceries #todo #personal #p2
/memory add Fix login bug #todo #work #p1
/memory add Read design doc #todo #work #p3
```

## Listing active tasks

```
/memory search todo
```

This returns all entries tagged `#todo`. Format them as a checklist for the user:
```
📋 Active Tasks:
  🔴 #3 [p1, work] Fix login bug
  🟡 #1 [p2, personal] Buy groceries
  🟢 #5 [p3, work] Read design doc
```

## Completing a task

When user says "done with X" or "完成了":

1. Find the task: `/memory search todo`
2. Edit to replace `#todo` with `#done`: `/memory edit <id> <same text> #done #work`

Or simply delete it: `/memory delete <id>`

## Filtering by context

- Work tasks: `/memory search todo` then filter for `#work` tags
- Urgent only: `/memory search p1`
- Personal: `/memory search personal`

## Tips

- When listing tasks, sort by priority (p1 → p2 → p3) in your response
- Suggest using `cron` tool for task reminders: "Want me to remind you about this in 2 hours?"
- Periodically suggest cleanup: "You have 3 completed tasks — want me to clean them up?"
