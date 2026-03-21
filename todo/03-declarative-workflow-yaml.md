---
title: "Декларативный workflow.yaml: гибкая настройка фаз и переходов"
status: done
priority: high
created: 2026-03-16
completed: 2026-03-21
---

# 03 — Декларативный workflow.yaml: гибкая настройка фаз и переходов

## Status: DONE (merged into main, commit 97c40a3)

## Context

Сейчас state machine (9 фаз, граф переходов, permissions per phase, side-effects) захардкожена в Go-коде.
Это означает, что любое изменение workflow (добавить фазу, изменить переход, поменять permissions) требует правки Go-кода, перекомпиляции и переразвёртывания.

**Цель:** Вынести определение workflow в единый `workflow.yaml`, чтобы:

- Фазы, переходы, permissions и side-effects определялись декларативно
- Можно было создавать разные workflow для разных проектов (override через `.wf-agents.yaml`)
- Go-код стал generic state machine executor

**Вдохновление:** Symphony's WORKFLOW.md (single file = source of truth для workflow), но адаптировано под Go/Temporal.

## Схема workflow.yaml

```yaml
# workflow.yaml — единый source of truth для workflow

# ─── Фазы ────────────────────────────────────────────────────────────────

phases:
  start: PLANNING
  stop: [COMPLETE]

  defaults:
    permissions:
      # Bash-команды, авто-одобряемые в любой фазе (read-only + тесты + линтеры).
      # Дополняются per-phase через whitelist.
      safe_commands:
        - ls
        - cat
        - head
        - tail
        - wc
        - file
        - grep
        - rg
        - awk
        - sort
        - uniq
        - diff
        - echo
        - printf
        - pwd
        - jq
        - yq
        - true
        - false
        - test
        - git status
        - git log
        - git diff
        - git show
        - git branch
        - git remote
        - git tag
        - git rev-parse
        - git ls-files
        - git blame
        - go test
        - go vet
        - go build
        - go list
        - go mod
        - golangci-lint
        - npm test
        - npm run lint
        - cargo test
        - cargo check
        - pytest
        - wf-client

      # ── Ролевые permissions ──────────────────────────────────────────
      #
      # Модель: deny = hook exit 2 → жёсткий отказ, инструмент не будет
      # выполнен, пользователя не спрашивают.
      #
      # В будущем: allow = авто-одобрение (hook exit 0), пользователя
      # не спрашивают. Пока поддерживаем только deny.
      #
      # file_writes — шорткат для [Edit, Write, NotebookEdit].
      # deny — список дополнительных инструментов (MCP и др.).

      # Team Lead никогда не редактирует файлы проекта напрямую.
      # Исключение: Claude infra files — обрабатывается в Go, не в YAML.
      lead:
        file_writes: deny

      # Teammates по умолчанию тоже не могут редактировать.
      # Фазы DEVELOPING и COMMITTING переопределяют это для developer*.
      teammate:
        - agent: "developer*"
          file_writes: deny
        - agent: "reviewer*"
          file_writes: deny

    # ── Idle rules (defaults) ──────────────────────────────────────────
    # По умолчанию idle разрешён для всех.
    # Фазы могут переопределить: deny или checks per agent.
    # "lead" — зарезервированное имя для тимлида, остальные — glob по имени агента.
    idle:
      - agent: lead
        deny: false
      - agent: "*"
        deny: false

  PLANNING:
    display: { label: "Planning", icon: "clipboard", color: "#6366f1" }
    instructions: planning.md
    hint: "Transition to RESPAWN first."
    idle:
      - agent: lead
        deny: true
        message: "No teammates in PLANNING — transition to BLOCKED before stopping"
    permissions:
      whitelist:
        - git checkout
        - git pull
        - git fetch
        - git stash list
        - git stash show
        - git stash pop
        - git stash apply
        - git stash drop
        - git describe
        - git ls-tree
        - git config
        - make
        - curl
        - wget
        - tree
        - stat
        - cd
        - du
        - df
        - env
        - date
        - hostname
        - whoami
        - uname
        - gh api
        - gh issue

  RESPAWN:
    display: { label: "Respawn", icon: "refresh", color: "#8b5cf6" }
    instructions: respawn.md
    hint: "Only agent management allowed. Transition to DEVELOPING when agents are ready."

  DEVELOPING:
    display: { label: "Developing", icon: "code", color: "#10b981" }
    instructions: developing.md
    on_enter:
      - type: increment_iteration
    permissions:
      # developer* может редактировать файлы в этой фазе.
      # Переопределяет deny из defaults.
      teammate:
        - agent: "developer*"
          file_writes: allow
    idle:
      - agent: "developer*"
        checks:
          - {
              type: command_ran,
              category: lint,
              message: "Run linter before going idle",
            }
          - {
              type: command_ran,
              category: test,
              message: "Run tests before going idle",
            }

  REVIEWING:
    display: { label: "Reviewing", icon: "search", color: "#f59e0b" }
    instructions: reviewing.md
    hint: "Delegate review to Reviewer teammate. Do NOT review code directly. If issues found, transition to DEVELOPING (not RESPAWN)."
    # teammate file_writes: deny наследуется из defaults — редактировать нельзя

  COMMITTING:
    display: { label: "Committing", icon: "git-commit", color: "#3b82f6" }
    instructions: committing.md
    hint: "Only git operations allowed."
    permissions:
      whitelist: [git add, git commit, git push]
      teammate:
        - agent: "developer*"
          file_writes: allow

  PR_CREATION:
    display:
      { label: "PR Creation", icon: "git-pull-request", color: "#ec4899" }
    instructions: pr_creation.md
    hint: "Only PR creation commands allowed."
    permissions:
      whitelist: [glab mr create, glab mr view, glab mr list]

  FEEDBACK:
    display: { label: "Feedback", icon: "message-circle", color: "#f97316" }
    instructions: feedback.md
    hint: "Triage PR comments. Accepted → implement first (RESPAWN→...→push), then reply what was done. Rejected → reply immediately with reasoning."
    idle:
      - agent: lead
        deny: true
        message: "Continue polling for feedback"

  COMPLETE:
    display: { label: "Complete", icon: "check-circle", color: "#22c55e" }
    instructions: complete.md

# ─── Переходы ────────────────────────────────────────────────────────────
# Все переходы доступны только тимлиду (main agent).
# BLOCKED неявно — из любой нетерминальной фазы и обратно.
# when — выражение-precondition, должно быть true для разрешения перехода.
#
# Доступные переменные (собираются hook-handler автоматически):
#   working_tree_clean  — git status (bool)
#   ci_passed           — CI pipeline / checks passed (bool)
#   review_approved     — MR/PR approved by reviewer (bool)
#   merged              — MR/PR merged (bool)
#   active_agents       — количество активных агентов (int)
#   iteration           — текущая итерация (int)
#   max_iterations      — лимит итераций (int)
#
# Примечание: имена переменных — целевые. При миграции Go-код
# переименует evidence-ключи (pr_checks_pass → ci_passed,
# pr_approved → review_approved, pr_merged → merged).
#
# PLANNING → RESPAWN → DEVELOPING → REVIEWING → COMMITTING → PR_CREATION → FEEDBACK → COMPLETE
#                ↑         ↑            |             |                         |
#                |         └── Rejected ┘             |                         |
#                └──────── Iterate ───────────────────┘──────── Rework ─────────┘

transitions:
  PLANNING:
    - to: RESPAWN
      when: working_tree_clean
      message: "working tree is not clean"

  RESPAWN:
    - to: DEVELOPING
      when: active_agents == 0
      message: "shut down old teammates first"

  DEVELOPING:
    - to: REVIEWING
      when: not working_tree_clean
      message: "no changes to review"

  REVIEWING:
    - to: COMMITTING
      label: "Approved"
    - to: DEVELOPING
      label: "Rejected"
      when: iteration < max_iterations
      message: "max iterations reached"

  COMMITTING:
    - to: RESPAWN
      label: "Iterate"
      when: working_tree_clean and iteration < max_iterations
      message: "working tree is not clean or max iterations reached"
    - to: PR_CREATION
      label: "Ready"
      when: working_tree_clean
      message: "working tree is not clean"

  PR_CREATION:
    - to: FEEDBACK
      when: ci_passed
      message: "CI has not passed — wait for pipeline"

  FEEDBACK:
    - to: COMPLETE
      label: "Done"
      when: review_approved or merged
      message: "PR has not been approved or merged"
    - to: RESPAWN
      label: "Rework"
      when: iteration < max_iterations
      message: "max iterations reached"

# ─── Tracking (категории команд для idle rules) ──────────────────────────

tracking:
  lint:
    patterns: [go vet, golangci-lint, npm run lint, cargo clippy, task lint]
    invalidate_on_file_change: true
  test:
    patterns:
      [go test, npm test, cargo test, python -m pytest, pytest, task test]
    invalidate_on_file_change: true
```

## Что НЕ выносится в YAML (остаётся в Go)

1. **BLOCKED** — инфраструктурная мета-фаза (auto-enter на PermissionRequest, auto-exit на PostToolUse, preBlockedPhase tracking)
2. **Claude infra file exemptions** — `.claude/plans/`, `.claude/projects/*/memory/` всегда exempt от file-write блокировок
3. **Bash command splitting** — логика разбора `&&`, `||`, `|`, `;`
4. **Team Lead detection** — teammate определяется по `session_id != workflow_session_id` (session ID из workflow ID не совпадает с текущим session ID хука)
5. **Bash prefix matching** — `matchesBashPrefix()`
6. **Agent lifecycle** — SubagentStop удаляет one-shot агентов (Explore и т.п.) при совпадении agent_id. Named teammates (`developer-*`, `reviewer-*`) НЕ удаляются при SubagentStop — они персистентны и удаляются только через `shut-down` или при входе в terminal фазу
7. **Очистка activeAgents при входе в terminal фазу** — автоматика, не конфигурация
8. **Read-only tools auto-approve** — механизм авто-одобрения для `read_only_tools` реализуется в Go (hook exit 0 без вопросов), YAML только определяет список

## Архитектурные решения

### Один файл вместо двух

Раньше предполагалось два файла: `workflow.yaml` (topology) и `defaults.yaml` (guards/idle/tracking). Объединили в один, потому что:

- Guards ссылаются на фазы и переходы — логично держать рядом
- Teammate permissions ссылаются на фазы — должны быть рядом с определениями
- Cross-validation между файлами исчезает
- ~200 строк YAML — не проблема

### Модель permissions: safe_commands + whitelist + ролевые ограничения

- **`safe_commands`** в defaults — bash-команды, разрешённые всегда на всех стадиях
- **`read_only_tools`** в defaults — Claude Code tools, авто-одобряемые в любой фазе
- **`whitelist`** per-phase — дополнительные bash-команды сверх safe_commands
- Нет whitelist → только safe_commands
- Если команды нет ни в safe_commands, ни в whitelist текущей фазы — hook exit 0, Claude Code спросит у пользователя (это поведение по умолчанию для неизвестных команд)
- **Ролевые ограничения** (`lead`/`teammate`) — `file_writes` как шорткат для `[Edit, Write, NotebookEdit]`, `deny` для произвольных инструментов (MCP и др.)
- **`deny`** = hook exit 2 → жёсткий отказ, инструмент не выполняется, пользователя не спрашивают. **`allow`** (будущее) = авто-одобрение (hook exit 0)

### Permissions: file_writes + deny (расширяемость)

`file_writes` — шорткат для самого частого кейса. Для других инструментов (MCP, будущие tools) используется generic `deny` список:

```yaml
teammate:
  - agent: "developer*"
    file_writes: deny # шорткат для Edit, Write, NotebookEdit
    deny: [mcp__jira__create_issue] # произвольные инструменты
```

Пока поддерживаем только `deny` (hook exit 2 — жёсткий отказ). В будущем `allow` добавит авто-одобрение (hook exit 0 — пользователя не спрашивают).

### Переменные в when-выражениях

Имена переменных в YAML (`ci_passed`, `review_approved`, `merged`) — целевые.
Текущий Go-код использует другие evidence-ключи (`pr_checks_pass`, `pr_approved`, `pr_merged`).
При миграции Go-код переименует ключи на целевые имена из YAML.

### Итерация: increment в DEVELOPING

YAML определяет `on_enter: increment_iteration` на фазе DEVELOPING.
Текущий Go-код инкрементирует при входе в RESPAWN (кроме первого из PLANNING и возврата из BLOCKED) + при REVIEWING→DEVELOPING.
При миграции Go-логика изменится на increment в DEVELOPING, как определено в YAML.

### Merge стратегия для проектных override'ов

В `.wf-agents.yaml` в корне проекта:

- Фазы: merge by name (override заменяет поля, не весь объект)
- Transitions: override полностью заменяет transitions для указанной фазы. Guard-условия (`when`) встроены в transitions — отдельной секции guards нет
- Tracking/idle/permissions: существующая логика merge из `merge.go`

## Файлы для изменения

### Новые файлы

| Файл                                     | Назначение                                                                                  |
| ---------------------------------------- | ------------------------------------------------------------------------------------------- |
| `internal/config/workflow.go`            | Типы: `WorkflowConfig`, `PhaseConfig`, `TransitionConfig`, `SideEffect`, `PhasePermissions` |
| `internal/config/workflow_validate.go`   | `ValidateWorkflowConfig()` — startup validation                                             |
| `internal/config/workflow_defaults.yaml` | Embedded default workflow.yaml (воспроизводит текущее поведение)                            |

### Модифицируемые файлы

| Файл                                  | Изменения                                                                                                                                     |
| ------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/model/state.go`             | Удалить константы фаз (кроме `PhaseBlocked`). `IsTerminal()` → dynamic lookup через `SetTerminalPhases()`                                     |
| `internal/workflow/guards.go`         | `validTransitions` → из config. `CheckToolPermission()` → читает permissions из config. `PhaseHint()` → из config. Удалить все hardcoded maps |
| `internal/workflow/coding_session.go` | Initial phase из config. Side-effects → generic executor. Переименовать evidence-ключи на целевые имена                                       |
| `cmd/hook-handler/main.go`            | Phase→instruction mapping → из config                                                                                                         |
| `cmd/worker/main.go`                  | Load + validate config at startup                                                                                                             |
| `cmd/web/main.go`                     | Новый endpoint `GET /api/workflow-config`                                                                                                     |
| `cmd/web/static/index.html`           | Fetch `/api/workflow-config` вместо hardcoded данных                                                                                          |
| `internal/config/config.go`           | Объединить с workflow types. Удалить отдельный `defaults.yaml`                                                                                |
| `internal/config/merge.go`            | Расширить merge для новых секций (phases, transitions)                                                                                        |

## Validation при старте

`ValidateWorkflowConfig()` возвращает `[]error` (все ошибки сразу):

1. **Structural:** `start` ссылается на определённую фазу, `stop` — на определённые фазы, нет фазы "BLOCKED"
2. **Graph:** все `to` в transitions ссылаются на определённые фазы; stop-фазы не имеют outgoing; граф connected (BFS от start достигает всех non-stop фаз)
3. **Transitions:** все when-выражения используют известные переменные; все message непустые при наличии when
4. **Side-effects:** известные типы (`increment_iteration`); on_enter ссылается на существующие фазы
5. **Instructions:** referenced `.md` файлы существуют (warning, не error)
6. **Permissions:** agent glob-паттерны валидны; teammate override фазы ссылаются на определённые фазы

## Инкрементальная миграция

### Phase 1: Типы + загрузка + валидация

- Создать `workflow.go`, `workflow_validate.go`, `workflow_defaults.yaml`
- Загружать и валидировать при старте, но НЕ использовать — Go-fallbacks работают
- **Тест:** unit tests на parsing + validation

### Phase 2: Transition graph + phases из config

- `validTransitions` → config-driven lookup
- `PhaseHint()` → из config
- start/stop phases → из config
- **Тест:** существующие `guards_test.go` проходят без изменений

### Phase 3: Permissions из config

- Все hardcoded maps → из config
- `CheckToolPermission()` → safe_commands + whitelist + ролевые ограничения
- Переименовать evidence-ключи (`pr_checks_pass` → `ci_passed`, `pr_approved` → `review_approved`, `pr_merged` → `merged`)
- **Тест:** существующие permission tests проходят без изменений

### Phase 4: Guards + tracking + idle объединение

- Перенести содержимое `defaults.yaml` в `workflow.yaml`
- Удалить `defaults.yaml`
- **Тест:** все тесты проходят без изменений

### Phase 5: Hook-handler + web dashboard

- Phase→instruction mapping → из config
- Frontend fetches `/api/workflow-config`
- **Тест:** E2E проверка

## Верификация

```bash
# После каждой фазы миграции:
go test ./internal/workflow/ -v
go test ./internal/model/ -v
go test ./internal/config/ -v

# Полный E2E:
make install && make worker &
# Запустить coding session через Claude Code
# Проверить что фазы/переходы/permissions работают идентично
```

## Идеи из Symphony (OpenAI)

Стоящие идеи, которые можно интегрировать в рамках этой архитектуры:

1. **Stall detection** — добавить `stall_timeout` в PhaseConfig. При превышении — автоматический переход в BLOCKED с оповещением.
