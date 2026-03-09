# 0.1 Миграция на формат Claude Code Plugin

## Цель

Перевести wf_agents с текущего подхода (setup-скрипт + внешние Go-бинарники) на официальный формат плагинов Claude Code. Установка должна быть одной командой:

```
/plugin marketplace add eklemin/wf-agents
```

## Мотивация

- Текущий подход требует `make build`, `setup-project.sh`, Docker, Temporal worker — высокий барьер входа
- Оригинальный проект NTCoding использует plugin формат — zero-config установка
- Plugin marketplace — стандартный канал распространения (9000+ плагинов)
- Claude Code автоматически загружает hooks, agents, commands из plugin-директории

## Целевая структура

```
wf-agents/
├── .claude-plugin/
│   ├── plugin.json              # Manifest: name, version, description
│   └── marketplace.json         # Metadata для marketplace
├── agents/
│   ├── feature-team-lead.md     # YAML frontmatter + prompt
│   ├── developer.md
│   └── reviewer.md
├── commands/
│   ├── start.md                 # /wf-agents:start — запуск сессии
│   ├── transition.md            # /wf-agents:transition — переход фазы
│   ├── status.md                # /wf-agents:status — текущее состояние
│   └── report.md                # /wf-agents:report — отчёт по сессии
├── hooks/
│   └── hooks.json               # Все хуки → src/shell.ts
├── states/
│   ├── planning.md
│   ├── respawn.md
│   ├── developing.md
│   ├── reviewing.md
│   ├── committing.md
│   ├── pr_creation.md
│   ├── feedback.md
│   └── blocked.md
├── src/                         # TypeScript — hook handler + workflow engine
│   ├── shell.ts                 # Единая точка входа для всех хуков
│   ├── workflow/
│   │   ├── engine.ts            # State machine, event sourcing
│   │   ├── states.ts            # Определения фаз + guards
│   │   ├── transitions.ts       # Валидация переходов
│   │   └── guards.ts            # Evidence-based guards
│   ├── hooks/
│   │   ├── pre-tool-use.ts      # Permission enforcement
│   │   ├── session-start.ts     # Инициализация сессии
│   │   ├── subagent.ts          # SubagentStart/Stop
│   │   └── auto-block.ts        # Auto-BLOCKED/unblock логика
│   ├── storage/
│   │   └── sqlite.ts            # Event store (SQLite, zero-config)
│   └── adapters/
│       └── temporal.ts          # Опциональный: forward событий в Temporal
├── CLAUDE.md                    # Инструкции для разработки самого плагина
└── package.json
```

## Этапы

### Этап 1: Scaffold плагина

- [ ] Создать `.claude-plugin/plugin.json` с манифестом
- [ ] Создать `package.json` с зависимостями (TypeScript, better-sqlite3)
- [ ] Настроить `tsconfig.json`
- [ ] Создать `src/shell.ts` — единый CLI entry point для хуков

### Этап 2: Перенос state machine на TypeScript

- [ ] Порт `internal/model/state.go` → `src/workflow/states.ts` (фазы, переходы)
- [ ] Порт `internal/workflow/guards.go` → `src/workflow/guards.ts` (evidence-based guards с OR-условиями)
- [ ] Порт `internal/workflow/coding_session.go` → `src/workflow/engine.ts` (event sourcing, переходы, activeAgents)
- [ ] SQLite event store вместо Temporal state

### Этап 3: Перенос hook handler

- [ ] Порт `cmd/hook-handler/main.go` → `src/hooks/*.ts`
- [ ] PreToolUse: whitelist PLANNING, RESPAWN write ban, global git restrictions
- [ ] Auto-BLOCKED/unblock: Stop/Notification/TeammateIdle → BLOCKED, активные события → unblock
- [ ] SubagentStart/Stop: tracking activeAgents
- [ ] SessionStart: инициализация + context injection
- [ ] Phase instruction injection через additionalContext

### Этап 4: Agents и Commands

- [ ] Конвертировать `templates/agents/*.md` в формат с YAML frontmatter для `agents/`
- [ ] Создать slash commands в `commands/` (start, transition, status, report)
- [ ] Перенести `states/*.md` — фазовые инструкции

### Этап 5: Temporal как опциональный адаптер

- [ ] `src/adapters/temporal.ts` — forward событий в Temporal если `TEMPORAL_HOST` задан
- [ ] Web dashboard остаётся отдельным (Go), работает с Temporal
- [ ] Без `TEMPORAL_HOST` — полностью standalone, только SQLite

### Этап 6: Тестирование и публикация

- [ ] Unit-тесты на state machine и guards
- [ ] Integration-тест: полный happy path через Claude Code test harness
- [ ] `.claude-plugin/marketplace.json` для публикации
- [ ] README с инструкциями установки

## Что сохраняем из текущего проекта

- Фазовая модель (PLANNING → ... → COMPLETE + BLOCKED)
- Evidence-based guards с OR-условиями (pr_approved OR pr_merged)
- Whitelist подход для PLANNING (только safe bash команды)
- Auto-BLOCKED/unblock логика
- RESPAWN как iteration boundary с проверкой activeAgents
- FEEDBACK протокол (валидация комментариев, явные ответы)
- Web dashboard (Temporal) — как отдельный optional компонент

## Что меняется

| Было | Станет |
|------|--------|
| `setup-project.sh` | `/plugin marketplace add eklemin/wf-agents` |
| Go бинарники (hook-handler, wf-client, worker) | TypeScript (`npx tsx src/shell.ts`) |
| Temporal обязателен | SQLite по умолчанию, Temporal опционально |
| `wf-client transition` | `/wf-agents:transition` slash command |
| Ручное копирование agents | Автоматическая загрузка из `agents/` |
| `settings.local.json` merge через jq | `hooks/hooks.json` в plugin — подхватывается автоматически |

## Ссылки

- [Claude Code Plugins Reference](https://code.claude.com/docs/en/plugins-reference)
- [Create Plugins](https://code.claude.com/docs/en/plugins)
- [NTCoding/autonomous-claude-agent-team](https://github.com/NTCoding/autonomous-claude-agent-team)
- [Hook-driven dev workflows — Nick Tune](https://nick-tune.me/blog/2026-02-28-hook-driven-dev-workflows-with-claude-code/)
