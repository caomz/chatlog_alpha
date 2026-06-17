#!/usr/bin/env bash
# scripts/ralph/test_branch_merge.sh
#
# US-003 沙箱回归脚本:在不污染真实仓库的前提下,验证 ralph.py 的
#   - ensure_work_branch() (US-001)
#   - auto_merge_branch()  (US-002)
# 在 3 个独立场景下闭环:
#   1. 干净合并(2 story 全 passes,合并后 base 含故事改动、HEAD 是 merge commit)
#   2. blocked 跳过(有 story blocked,auto_merge_branch 早退,base HEAD 不变)
#   3. conflict abort(work-branch 与 base 同文件冲突,git merge --abort 还原)
#
# 设计约束(progress.txt Codebase Patterns):
#   - 沙箱自包含 import: 复制 ralph.py + dashboard.py 到 <sandbox>/scripts/ralph/,
#     `import ralph` 时 __file__ 指向沙箱副本,PROJECT_ROOT 自动解析到沙箱根,
#     无需 monkey-patch。
#   - macOS mktemp 返回 /var/folders/...(symlink),git rev-parse --show-toplevel
#     返回 /private/var/folders/...(物理路径),必须用
#     `cd "$(pwd)" && pwd -P` 拿物理路径再比 toplevel。
#   - WORK_BRANCH / BASE_BRANCH 是模块级全局,ensure+story_commit+auto_merge
#     必须在**同一 python3 进程**内顺序调用,跨进程会被 GC 重置为 None。
#
# 每个场景独立 mktemp -d 沙箱 + 独立 python3 进程,脚本顶层 trap EXIT 兜底
# 清理所有登记的沙箱目录(兼容 macOS /bin/bash 3.2,不支持 RETURN trap)。

set -euo pipefail

RALPH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REAL_RALPH_PY="$RALPH_DIR/ralph.py"
REAL_DASHBOARD_PY="$RALPH_DIR/dashboard.py"

if [ ! -f "$REAL_RALPH_PY" ] || [ ! -f "$REAL_DASHBOARD_PY" ]; then
  echo "❌ 找不到 ralph.py / dashboard.py: $RALPH_DIR" >&2
  exit 1
fi

# macOS bash 3.2 不支持 trap ... RETURN;改用全局 SBX_DIRS 数组 + EXIT trap
SBX_DIRS=()

cleanup_all() {
  local dir
  for dir in "${SBX_DIRS[@]:-}"; do
    if [ -n "$dir" ] && [ -d "$dir" ]; then
      chmod -R u+w "$dir" 2>/dev/null || true
      rm -rf "$dir" 2>/dev/null || true
    fi
  done
}

trap cleanup_all EXIT

SCRIPT_ID="us003-$$"
PASSED=0
FAILED=0
SCENARIO_TOTAL=3

# ---------- helpers ----------

# SBX_DIRS+=("$sbx") 后统一由 EXIT trap 清理,这里不再需要单点 cleanup。

# 构造沙箱: <sandbox_dir> 必须是空目录
# 在 <sandbox_dir> 内:
#   - git init (默认分支 = git config init.defaultBranch 或 master/main)
#   - 创建 initial commit(空 README)
#   - mkdir -p scripts/ralph 并复制 ralph.py + dashboard.py
#   - 写最小 prd.json(branchName 来自 $1)
# 输出: 沙箱物理路径 + 初始 HEAD 哈希 + base 分支名
init_sandbox() {
  local sbx="$1"
  local branch_name="$2"

  cd "$sbx" || return 1
  # 显式沙箱 PWD 守卫:确保下面所有 git 写操作都落在 mktemp 沙箱内,
  # 而不是误在真实仓库执行。AC#5 "git 写操作前断言 $PWD 位于沙箱" 字面约束。
  guard_sandbox_pwd "$sbx" "init_sandbox git init"
  git init -q
  # 让 git init 不再 warning,直接给一个 defaultBranch
  git checkout -q -b main 2>/dev/null || git symbolic-ref HEAD refs/heads/master 2>/dev/null || true
  # 切到一个稳定 base 分支名(sandbox 里 git init 默认分支不可控,统一改 main)
  # 兼容 main 已被创建的情况
  local current_default
  current_default="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo master)"
  if [ "$current_default" != "main" ]; then
    git branch -m "$current_default" main 2>/dev/null || true
    git checkout -q -b main 2>/dev/null || true
  fi

  # initial commit
  : > README.md
  git add README.md
  git -c user.email=test@local -c user.name=test commit -q -m "init"

  # scripts/ralph 目录 + 复制 ralph.py + dashboard.py
  mkdir -p scripts/ralph
  cp "$REAL_RALPH_PY" scripts/ralph/ralph.py
  cp "$REAL_DASHBOARD_PY" scripts/ralph/dashboard.py
  git add scripts/ralph/ralph.py scripts/ralph/dashboard.py
  git -c user.email=test@local -c user.name=test commit -q -m "chore: copy ralph harness"

  # 物理路径(给后续断言用)
  local physical_toplevel
  physical_toplevel="$(cd "$(pwd)" && pwd -P)"
  local base_branch
  base_branch="$(git rev-parse --abbrev-ref HEAD)"
  local base_head
  base_head="$(git rev-parse HEAD)"

  # 写最小 prd.json(branchName 由调用方传)
  cat > scripts/ralph/prd.json <<EOF
{
  "branchName": "${branch_name}",
  "userStories": []
}
EOF
  # prd.json 暂不 commit(让 ensure_work_branch 看到未 tracked 但 in workdir 的状态即可)
  # 关键: prd.json 不污染 base 分支的"初始状态",所以 git status 会有 ?? 噪音
  # 这是预期的(沙箱特性,真实仓库 prd.json 早就 tracked)。

  echo "$physical_toplevel|$base_branch|$base_head"
}

# 物理路径断言: $1 = git rev-parse --show-toplevel, $2 = 期望物理路径
# 沙箱里 git rev-parse 永远走物理路径(/.git 真实路径),直接相等即可
assert_toplevel() {
  local sbx="$1"
  local got
  got="$(cd "$sbx" && git rev-parse --show-toplevel)"
  local want
  want="$(cd "$sbx" && pwd -P)"
  if [ "$got" = "$want" ]; then
    echo "  ✓ toplevel 物理路径一致: $got"
    return 0
  fi
  echo "  ✗ toplevel 物理路径不一致: got=$got want=$want" >&2
  return 1
}

# 沙箱 PWD 守卫:在每个 git 写操作前显式断言 $PWD 位于指定 mktemp 沙箱内。
# AC#5 "脚本任何 git 写操作前断言 $PWD 位于沙箱目录" 的字面硬约束。
# 比较走物理路径(cd + pwd -P),避开 macOS /var/folders/... symlink 噪音。
# 不在沙箱内立即 echo 红字 + exit 1,避免污染真实仓库。
guard_sandbox_pwd() {
  local sbx="$1"
  local label="${2:-git 写操作}"
  local cur
  cur="$(pwd -P)"
  local want
  want="$(cd "$sbx" && pwd -P)"
  if [ "$cur" != "$want" ]; then
    echo "  ✗ 沙箱 PWD 守卫失败: $label 之前 PWD=$cur,期望 $want" >&2
    echo "  ✗ 拒绝执行任何 git 写操作,避免污染真实仓库" >&2
    exit 1
  fi
}

run_scenario() {
  local name="$1"
  local fn="$2"
  echo
  echo "================================================================"
  echo "场景: $name"
  echo "================================================================"
  if "$fn"; then
    echo "✅ 场景通过: $name"
    PASSED=$((PASSED + 1))
  else
    echo "❌ 场景失败: $name"
    FAILED=$((FAILED + 1))
  fi
}

# ---------- 场景 1: 干净合并 ----------
# 2 story 全 passes=true,ensure_work_branch + 2 次手动模拟 story commit +
# auto_merge_branch 全在同一 python3 进程内,断言 base 含 merge commit
scenario_clean_merge() {
  local sbx
  sbx="$(mktemp -d -t "${SCRIPT_ID}-clean-XXXXXX")"
  SBX_DIRS+=("$sbx")

  local branch_name="ralph/sbx-clean"
  local meta physical base_branch base_head
  meta="$(init_sandbox "$sbx" "$branch_name")"
  IFS='|' read -r physical base_branch base_head <<<"$meta"
  echo "  沙箱: $physical"
  echo "  base: $base_branch  initial HEAD: $base_head"

  # 更新 prd.json:2 story 全 passes
  cat > "$sbx/scripts/ralph/prd.json" <<EOF
{
  "branchName": "${branch_name}",
  "userStories": [
    {"id": "STORY-1", "title": "story one", "passes": true, "blocked": false, "notes": ""},
    {"id": "STORY-2", "title": "story two", "passes": true, "blocked": false, "notes": ""}
  ]
}
EOF

  # 同一 python3 进程内:ensure_work_branch + 2 次 story commit + auto_merge_branch
  # branch_name 用 RALPH_TEST_BRANCH env 传,避免污染 sys.argv(ralph.py 把
  # POSITIONAL_ARGS[0] 当 AGENT,非 claude/codex 会 sys.exit(2))
  ( cd "$sbx" && guard_sandbox_pwd "$sbx" "scenario_clean_merge python 启动" && RALPH_TEST_BRANCH="$branch_name" PYTHONDONTWRITEBYTECODE=1 python3 <<'PY'
import os
import sys
import subprocess
from pathlib import Path

# 关键:确保 import 时 __file__ 解析到沙箱副本
sys.path.insert(0, str(Path("scripts/ralph").resolve()))
import ralph  # noqa: E402

# 1) ensure_work_branch
sbx_branch = os.environ["RALPH_TEST_BRANCH"]
got = ralph.ensure_work_branch()
assert got == sbx_branch, f"ensure_work_branch 返 {got!r}, 期望 {sbx_branch!r}"
assert ralph.WORK_BRANCH == sbx_branch, f"WORK_BRANCH={ralph.WORK_BRANCH!r}"
assert ralph.BASE_BRANCH is not None, "BASE_BRANCH 未捕获"
print(f"  ✓ ensure_work_branch: work={ralph.WORK_BRANCH} base={ralph.BASE_BRANCH}")

# 2) 模拟 2 个 story commit
for sid in ("STORY-1", "STORY-2"):
    Path(f"story-{sid}.txt").write_text(f"content of {sid}\n", encoding="utf-8")
    subprocess.run(["git", "add", f"story-{sid}.txt"], check=True)
    subprocess.run(
        ["git", "-c", "user.email=test@local", "-c", "user.name=test",
         "commit", "-q", "-m", f"feat: {sid}"],
        check=True,
    )
print("  ✓ 2 story commit 完成")

# 3) auto_merge_branch
ok = ralph.auto_merge_branch()
assert ok is True, "auto_merge_branch 应该返 True"
print("  ✓ auto_merge_branch 成功")
PY
  )
  local rc=$?
  if [ $rc -ne 0 ]; then
    echo "  ✗ 沙箱 python 进程退出码非零: $rc" >&2
    return 1
  fi

  # 断言:base HEAD 是 merge commit
  ( cd "$sbx" && assert_toplevel "$sbx" ) || return 1
  local current_branch
  current_branch="$(cd "$sbx" && git rev-parse --abbrev-ref HEAD)"
  if [ "$current_branch" != "$base_branch" ]; then
    echo "  ✗ 合并后 HEAD 应在 base ($base_branch),实际在 $current_branch" >&2
    return 1
  fi
  echo "  ✓ 合并后 HEAD 切回 base: $current_branch"

  local merge_count
  merge_count="$(cd "$sbx" && git log --oneline | grep -c 'merge: ralph/sbx-clean')"
  if [ "$merge_count" = "0" ]; then
    echo "  ✗ base log 缺 merge: ralph/sbx-clean commit" >&2
    ( cd "$sbx" && git log --oneline ) >&2
    return 1
  fi
  echo "  ✓ base log 含 merge commit"

  # 确认 merge commit 是 2 parents(--no-ff 标志)
  local parents
  parents="$(cd "$sbx" && git log -1 --format='%P' | wc -w | tr -d ' ')"
  if [ "$parents" != "2" ]; then
    echo "  ✗ merge commit 应有 2 parents(--no-ff),实际 $parents" >&2
    return 1
  fi
  echo "  ✓ merge commit 有 2 parents(--no-ff 正确)"

  # work-branch 仍存在
  if ! ( cd "$sbx" && git rev-parse --verify --quiet "refs/heads/$branch_name" >/dev/null ); then
    echo "  ✗ work-branch $branch_name 已不存在(应保留)" >&2
    return 1
  fi
  echo "  ✓ work-branch 保留: $branch_name"

  # base 上 story 文件可见
  for sid in STORY-1 STORY-2; do
    if [ ! -f "$sbx/story-$sid.txt" ]; then
      echo "  ✗ base 分支缺 story-$sid.txt(应包含故事改动)" >&2
      return 1
    fi
  done
  echo "  ✓ base 包含 2 个 story 改动"

  return 0
}

# ---------- 场景 2: blocked 跳过 ----------
# 1 story passes + 1 story blocked,auto_merge_branch 应该早退并 return False,
# base HEAD 不变(无 merge commit),当前分支仍应在 work-branch
scenario_blocked_skip() {
  local sbx
  sbx="$(mktemp -d -t "${SCRIPT_ID}-blocked-XXXXXX")"
  SBX_DIRS+=("$sbx")

  local branch_name="ralph/sbx-blocked"
  local meta
  meta="$(init_sandbox "$sbx" "$branch_name")"
  IFS='|' read -r physical base_branch base_head <<<"$meta"
  echo "  沙箱: $physical"
  echo "  base: $base_branch  initial HEAD: $base_head"

  cat > "$sbx/scripts/ralph/prd.json" <<EOF
{
  "branchName": "${branch_name}",
  "userStories": [
    {"id": "STORY-A", "title": "passes", "passes": true, "blocked": false, "notes": ""},
    {"id": "STORY-B", "title": "blocked", "passes": false, "blocked": true, "notes": "blocked reason"}
  ]
}
EOF

  ( cd "$sbx" && guard_sandbox_pwd "$sbx" "scenario_blocked_skip python 启动" && RALPH_TEST_BRANCH="$branch_name" PYTHONDONTWRITEBYTECODE=1 python3 <<'PY'
import os
import sys
import subprocess
from pathlib import Path

sys.path.insert(0, str(Path("scripts/ralph").resolve()))
import ralph  # noqa: E402

sbx_branch = os.environ["RALPH_TEST_BRANCH"]
got = ralph.ensure_work_branch()
assert got == sbx_branch

# 1 个 story commit
Path("story-a.txt").write_text("ok\n", encoding="utf-8")
subprocess.run(["git", "add", "story-a.txt"], check=True)
subprocess.run(
    ["git", "-c", "user.email=test@local", "-c", "user.name=test",
     "commit", "-q", "-m", "feat: STORY-A"],
    check=True,
)

ok = ralph.auto_merge_branch()
assert ok is False, f"blocked story 存在时 auto_merge_branch 应返 False,实际 {ok!r}"
print("  ✓ auto_merge_branch 早退返 False(blocked 列表非空)")
PY
  )
  local rc=$?
  if [ $rc -ne 0 ]; then
    echo "  ✗ 沙箱 python 进程退出码非零: $rc" >&2
    return 1
  fi

  ( cd "$sbx" && assert_toplevel "$sbx" ) || return 1

  # base HEAD 不变(commit count 与 base_head 哈希一致)
  local new_base_head
  new_base_head="$(cd "$sbx" && git rev-parse "$base_branch")"
  if [ "$new_base_head" != "$base_head" ]; then
    echo "  ✗ base HEAD 漂移:was=$base_head now=$new_base_head" >&2
    return 1
  fi
  echo "  ✓ base HEAD 不变(blocked 跳过未合并)"

  # 无 merge commit
  local merge_count
  merge_count="$(cd "$sbx" && git log --all --oneline | grep -c '^.*merge: ')"
  if [ "$merge_count" != "0" ]; then
    echo "  ✗ 出现意外 merge commit(count=$merge_count)" >&2
    ( cd "$sbx" && git log --all --oneline ) >&2
    return 1
  fi
  echo "  ✓ 无 merge commit"

  # 当前分支 = work-branch(没切 base)
  local current_branch
  current_branch="$(cd "$sbx" && git rev-parse --abbrev-ref HEAD)"
  if [ "$current_branch" != "$branch_name" ]; then
    echo "  ✗ blocked 跳过场景当前分支应仍在 work-branch($branch_name),实际 $current_branch" >&2
    return 1
  fi
  echo "  ✓ 当前分支仍为 work-branch: $current_branch"

  return 0
}

# ---------- 场景 3: conflict abort ----------
# work-branch 与 base 在同一文件 shared.txt 上各自 commit 制造冲突,
# auto_merge_branch 应该冲突 → git merge --abort → 切回 work-branch → sys.exit(1)
# 验收:无残留 merge commit,work-branch 完好,冲突文件列表打印
scenario_conflict_abort() {
  local sbx
  sbx="$(mktemp -d -t "${SCRIPT_ID}-conflict-XXXXXX")"
  SBX_DIRS+=("$sbx")

  local branch_name="ralph/sbx-conflict"
  local meta
  meta="$(init_sandbox "$sbx" "$branch_name")"
  IFS='|' read -r physical base_branch base_head <<<"$meta"
  echo "  沙箱: $physical"
  echo "  base: $base_branch  initial HEAD: $base_head"

  cat > "$sbx/scripts/ralph/prd.json" <<EOF
{
  "branchName": "${branch_name}",
  "userStories": [
    {"id": "STORY-X", "title": "conflict path", "passes": true, "blocked": false, "notes": ""}
  ]
}
EOF

  # 不在 base 上先 commit shared.txt(否则 work-branch checkout -b 时已经继承,
  # merge 会 fast-forward,不冲突)。改在 python heredoc 内分两阶段:先 work-branch
  # 写 v1,然后切回 base 写 v2(共同祖先都没有 shared.txt),merge 时才是真冲突。
  # 用 `(...)` 进程分组把输出重定向到临时文件,与本脚本另外两个场景
  # (`scenario_clean_merge`/`scenario_blocked_skip`) 的括号分组 + 内联输出风格
  # 保持一致;避免 `$(...)` 命令替换内嵌套 `<<` heredoc 在 bash 3.2 (macOS 系统
  # bash) parse 阶段报 "syntax error near unexpected token `(`"。
  local out_file="$sbx/.scenario3_out.txt"
  : > "$out_file"
  local rc=0
  set +e
  ( cd "$sbx" && guard_sandbox_pwd "$sbx" "scenario_conflict_abort python 启动" && RALPH_TEST_BRANCH="$branch_name" PYTHONDONTWRITEBYTECODE=1 python3 <<'PY' ) >>"$out_file" 2>&1
import os
import sys
import subprocess
from pathlib import Path

sys.path.insert(0, str(Path("scripts/ralph").resolve()))
import ralph  # noqa: E402

sbx_branch = os.environ["RALPH_TEST_BRANCH"]
got = ralph.ensure_work_branch()
assert got == sbx_branch

base_branch = ralph.BASE_BRANCH
assert base_branch is not None

# 1) work-branch 上先创建 shared.txt v1
Path("shared.txt").write_text("work side content v1\n", encoding="utf-8")
subprocess.run(["git", "add", "shared.txt"], check=True)
subprocess.run(
    ["git", "-c", "user.email=test@local", "-c", "user.name=test",
     "commit", "-q", "-m", "work: add shared.txt v1"],
    check=True,
)

# 2) 切回 base,在 base 上写 shared.txt v2(共同祖先都没 shared.txt)
subprocess.run(["git", "checkout", "-q", base_branch], check=True)
Path("shared.txt").write_text("base side content v2\n", encoding="utf-8")
subprocess.run(["git", "add", "shared.txt"], check=True)
subprocess.run(
    ["git", "-c", "user.email=test@local", "-c", "user.name=test",
     "commit", "-q", "-m", "base: add shared.txt v2"],
    check=True,
)

# 3) 切回 work-branch(冲突发生在 work-branch HEAD 与 base HEAD 之间的三方合并)
subprocess.run(["git", "checkout", "-q", sbx_branch], check=True)

# auto_merge_branch 应该冲突 → abort → 切回 work → sys.exit(1)
try:
    ralph.auto_merge_branch()
except SystemExit as e:
    print(f"  ✓ auto_merge_branch 抛 SystemExit({e.code})")
    sys.exit(0)
print("  ✗ auto_merge_branch 应该 sys.exit(1),实际没抛", file=sys.stderr)
sys.exit(2)
PY
  rc=$?
  set -e
  local out
  out="$(cat "$out_file")"
  echo "$out"
  if [ "$rc" != "0" ] && [ "$rc" != "1" ]; then
    echo "  ✗ 沙箱 python 进程异常退出: rc=$rc" >&2
    return 1
  fi

  ( cd "$sbx" && assert_toplevel "$sbx" ) || return 1

  # base HEAD 不变
  local new_base_head
  new_base_head="$(cd "$sbx" && git rev-parse "$base_branch")"
  if [ "$new_base_head" != "$base_head" ]; then
    # 注意:base 此刻有 "base: add shared.txt" 这一 commit(我们故意制造的),但**与
    # 初始 base_head 之差只应是这一 commit**。不能用 === 比较,改成"没有意外的
    # 第二个 commit(没有 merge commit,也没有 feature 改动)":查 base log。
    :
  fi
  # 改用更稳的断言:base log 中除了 "init" / "chore: copy ralph harness" /
  # "base: add shared.txt" 之外,不应有 merge commit 或 work-side 提交。
  local base_merge_count
  base_merge_count="$(cd "$sbx" && git log --oneline "$base_branch" | grep -c 'merge: ')"
  if [ "$base_merge_count" != "0" ]; then
    echo "  ✗ base log 出现 merge commit(count=$base_merge_count)" >&2
    ( cd "$sbx" && git log --oneline "$base_branch" ) >&2
    return 1
  fi
  echo "  ✓ base log 无 merge commit"

  # work-branch 仍存在
  if ! ( cd "$sbx" && git rev-parse --verify --quiet "refs/heads/$branch_name" >/dev/null ); then
    echo "  ✗ work-branch $branch_name 已不存在(应保留)" >&2
    return 1
  fi
  echo "  ✓ work-branch 保留: $branch_name"

  # 无残留 merge state(.git/MERGE_HEAD 不存在)
  if [ -f "$sbx/.git/MERGE_HEAD" ]; then
    echo "  ✗ .git/MERGE_HEAD 残留(merge --abort 没成功)" >&2
    return 1
  fi
  echo "  ✓ 无 MERGE_HEAD 残留"

  # 当前 HEAD 在 work-branch(冲突 abort 后应切回)
  local current_branch
  current_branch="$(cd "$sbx" && git rev-parse --abbrev-ref HEAD)"
  if [ "$current_branch" != "$branch_name" ]; then
    echo "  ✗ 冲突 abort 后 HEAD 应在 work-branch($branch_name),实际 $current_branch" >&2
    return 1
  fi
  echo "  ✓ HEAD 在 work-branch"

  # 冲突文件列表打印(子进程输出应含 shared.txt)
  if ! echo "$out" | grep -q "shared.txt"; then
    echo "  ✗ 沙箱输出没打印冲突文件 shared.txt" >&2
    return 1
  fi
  echo "  ✓ 冲突文件列表打印: shared.txt"

  # 全局 log --all 不应有 merge commit
  local all_merge_count
  all_merge_count="$(cd "$sbx" && git log --all --oneline | grep -c 'merge: ')"
  if [ "$all_merge_count" != "0" ]; then
    echo "  ✗ git log --all 出现 merge commit(abort 不干净)" >&2
    ( cd "$sbx" && git log --all --oneline ) >&2
    return 1
  fi
  echo "  ✓ git log --all 无 merge commit(abort 干净)"

  return 0
}

# ---------- main ----------

run_scenario "干净合并(2 story 全 passes)" scenario_clean_merge
run_scenario "blocked 跳过(1 blocked + 1 passes)" scenario_blocked_skip
run_scenario "conflict abort(work vs base 同文件冲突)" scenario_conflict_abort

echo
echo "================================================================"
echo "总览: PASSED=$PASSED  FAILED=$FAILED  TOTAL=$SCENARIO_TOTAL"
echo "================================================================"

if [ "$FAILED" = "0" ] && [ "$PASSED" = "$SCENARIO_TOTAL" ]; then
  echo "ALL BRANCH MERGE TESTS PASSED"
  exit 0
fi
echo "BRANCH MERGE TESTS FAILED"
exit 1
