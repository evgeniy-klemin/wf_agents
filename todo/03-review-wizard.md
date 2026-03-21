---
title: Review Wizard: 03-declarative-workflow-yaml.md
status: done
completed: 2026-03-21
note: Все решения применены в рамках 03-declarative-workflow-yaml
---

# Review Wizard: 03-declarative-workflow-yaml.md

Для каждого пункта укажи вариант (a/b/c) или свой комментарий.

---

## 1. `guards` vs `transitions.when`

В YAML guards встроены в `transitions` через `when`. Но Merge и Validation описывают `guards` как отдельную секцию с `from`/`to`.

- **a)** Обновить Merge/Validation под новую модель (guards внутри transitions)
- **b)** Вернуть отдельную секцию `guards` в YAML
- **c)** Другое: \_\_\_

Ответ: a

---

## 2. `file_writes` vs `teammate_permissions`

`file_writes: deny/allow` в phase config конфликтует с `teammate_permissions` (developer\* разрешён в DEVELOPING + COMMITTING). Два механизма для одного.

- **a)** Убрать `file_writes` из phase config, оставить только `teammate_permissions`
- **b)** Убрать `teammate_permissions`, расширить `file_writes` per-phase
- **c)** Оставить оба, уточнить приоритет
- **d)** Другое: Предложи вариант, как сделать прозрачно и понятно.Чтоб конфиг удобно читался и было однозначное понимание.

Ответ: d

---

## 3. Нет `hint` для REVIEWING

В Go есть подробный hint (делегировать Reviewer teammate, не ревьюить напрямую). В YAML — пропущен.

- **a)** Добавить hint для REVIEWING в YAML
- **b)** Оставить без hint (детали в instructions/reviewing.md)
- **c)** Другое: \_\_\_

Ответ: a

---

## 4. Итерация: DEVELOPING vs RESPAWN (критическое)

YAML: `on_enter: increment_iteration` на DEVELOPING.
Go: инкремент при входе в RESPAWN (кроме первого из PLANNING / возврата из BLOCKED) + при REVIEWING→DEVELOPING.

- **a)** Исправить YAML под текущую Go-логику (increment на RESPAWN + на переходе REVIEWING→DEVELOPING)
- **b)** Оставить YAML как есть — при миграции Go-логика изменится на increment в DEVELOPING
- **c)** Другое: \_\_\_

Ответ: b

---

## 5. Имена переменных в `when`

YAML: `ci_passed`, `review_approved`, `merged`.
Go: `pr_checks_pass`, `pr_approved`, `pr_merged`.

- **a)** Исправить YAML на текущие Go-имена (`pr_checks_pass`, `pr_approved`, `pr_merged`)
- **b)** Оставить YAML-имена как целевые, при миграции переименовать в Go
- **c)** Другое: \_\_\_

Ответ: b

---

## 6. PLANNING whitelist неполный

В Go для PLANNING дополнительно разрешены: `make`, `curl`, `wget`, `tree`, `gh api`, `gh issue`, `du`, `df`, `stat`, `cd`, `env`, `date`, `hostname`, `whoami`, `uname` и другие.

- **a)** Дополнить YAML полным списком из Go
- **b)** Оставить сокращённый список — это сознательная чистка
- **c)** Другое: \_\_\_

Ответ: a

---

## 7. Read-only tools не описаны

`Read, Glob, Grep, WebFetch, WebSearch, ToolSearch, LSP` авто-одобряются в любой фазе, но YAML описывает только Bash-команды.

- **a)** Добавить секцию `read_only_tools` в defaults YAML
- **b)** Отнести к Go-инфраструктуре (добавить в секцию «что НЕ выносится в YAML»)
- **c)** Другое: \_\_\_

Ответ: a

---

## 8. PhaseHint-ы сильно укорочены

RESPAWN, REVIEWING, FEEDBACK в Go значительно подробнее.

- **a)** Перенести полные тексты из Go в YAML
- **b)** Оставить краткие — детали в `instructions/*.md`
- **c)** Другое: Проверить, что можно полезного добавить из go.

Ответ: c

---

## 9. SubagentStop: неточное описание

Документ: «SubagentStop не удаляет агентов». Реальность: one-shot агенты удаляются, named teammates (developer-_, reviewer-_) — нет.

- **a)** Исправить формулировку на точную
- **b)** Оставить упрощённую
- **c)** Другое: \_\_\_

Ответ: a

---

## 10. Team Lead detection

Документ: `agentID != "" → teammate`. Go: `session_id != workflow_session_id`.

- **a)** Исправить на реальный механизм
- **b)** Оставить упрощённый (это концептуальное описание)
- **c)** Другое: \_\_\_

Ответ: a

---

## 11. Глобально запрещённые git-команды

В Go: `git commit, git push, git checkout, git add` запрещены глобально с per-phase exemptions. YAML-модель (safe_commands + whitelist) не покрывает паттерн «запрещено везде, кроме...».

- **a)** Добавить секцию `forbidden_commands` с exemptions в YAML
- **b)** Считать что whitelist per-phase достаточен (если нет в safe_commands и не в whitelist — запрещено)
- **c)** Другое: \_\_\_

Ответ: b

---

## 12. Названия сигналов

Документ: `deregister-all-agents`, `shut-down`. Go: `clear-active-agents`, `agent-shut-down`.

- **a)** Исправить на актуальные из Go
- **b)** Оставить как есть (при миграции переименуем)
- **c)** Другое: В документе оставить только упоминания shut-down.

Ответ: c
