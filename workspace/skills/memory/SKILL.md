---
name: memory
description: Manage the agent's long-term memory — add, search, list, edit, delete tagged memory entries. Use when user says 记住 remember 记忆 memory forget 忘记, or when you need to persist important information across conversations.
---

# Memory Management

PicoClaw has a built-in memory system backed by SQLite (`workspace/memory.db`). Memory entries are tagged for retrieval by the Analyser (Phase 1).

## How memory works

1. **Automatic retrieval**: The Analyser extracts tags from every user message and retrieves matching memory entries before the main LLM responds. You don't need to manually search — relevant memories are injected automatically.
2. **Manual management**: Users can use `/memory` commands or ask you to remember/forget things.
3. **Background extraction**: The MemoryDigestWorker periodically extracts key facts from conversations into memory entries.

## Slash commands

```
/memory list              — show recent 10 memories
/memory add <text> #tags  — add a memory entry with tags
/memory delete <id>       — delete a memory by ID
/memory edit <id> <text> #tags — update content and tags
/memory search <tag1> [tag2]  — search by tags (OR logic)
/memory stats             — show entry count and all tags
```

## When to actively save memories

Save a memory when:
- User explicitly says "记住" / "remember" / "save this"
- User shares personal preferences, names, habits
- User provides project-specific context (API keys, endpoints, conventions)
- Important decisions or conclusions are reached
- User corrects you — save the correction

Do NOT save:
- Trivial chat / greetings
- Temporary one-off questions
- Information already in memory (check first with `/memory search`)

## Tag naming rules

- **Lowercase**, hyphen-separated: `project-name`, `user-preference`, `api-config`
- **Keep tags short** and reusable: `golang`, `work`, `health`, not `golang-programming-language`
- **Max 3-5 tags** per entry
- **Use existing tags** when possible — check with `/memory stats` to see available tags before inventing new ones

## Examples

User: "记住我用的是 MacBook Pro M3"
```
/memory add User uses MacBook Pro M3 #user-device #hardware
```

User: "我们项目的 API endpoint 是 https://api.example.com"
```
/memory add Project API endpoint: https://api.example.com #project #api-config
```

User: "以后回复我用中文"
```
/memory add User prefers Chinese (中文) responses #user-preference #language
```

User: "忘掉之前说的 API 地址"
```
/memory search api-config
# Find the entry ID, then:
/memory delete <id>
```

## Helping users organize memories

When user asks to review or clean up memories:
1. Run `/memory list` to show all entries
2. Identify duplicates or outdated entries
3. Suggest deletions or edits
4. Propose better tags for poorly-tagged entries
