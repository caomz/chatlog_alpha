# Feedback and Harness Audit

一句话结论：反馈子系统通常投入最少、回报最高；先把验证命令写清楚。

## Anti-False-Done Rules

- Do not claim completion from implementation alone.
- Do not claim completion from generated files alone.
- Do not claim completion from aggregate counters alone.
- Do not claim completion when verification failed or was skipped.
- Do not start broad refactors before core behavior verification passes.

## Useful Failure Records

Record failures with enough detail for self-correction:

- Exact command
- Working directory
- Exit code
- Key error line
- Suspected cause
- Next concrete fix

Bad feedback: `错了`.

Good feedback: `go test ./internal/chatlog/semantic failed because TestMiniMaxKeyPool expected 429 failover; inspect internal/chatlog/semantic/client.go retry branch.`

## Control-Variable Method

To quantify harness subsystem contribution:

1. Keep the task constant.
2. Change one subsystem at a time: instruction, tool, environment, state, or feedback.
3. Compare failure records and recovery time.
4. Attribute bottlenecks from evidence, not from intuition.

拆除实验只能提供线索，不能单独证明真正瓶颈。定位瓶颈要结合失败记录、归因和恢复成本。

## Harness Debt

Harness decays like code.

Audit regularly:

- Are commands still valid?
- Are docs still aligned with code?
- Are privacy/quota warnings current?
- Are handoff rules actually used?
- Are agents still failing in the same way?
