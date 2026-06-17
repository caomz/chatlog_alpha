#!/usr/bin/env python3
"""
Ralph - 自主 AI Agent 循环执行器（含 Validator）
"""

import json
import os
import sys
import subprocess
import time
from pathlib import Path

import dashboard

# 配置
MAX_ITERATIONS = 50
TIMEOUT_SECONDS = 30 * 60

# Agent 选择：支持 "claude"（默认）或 "codex"
# 用法：python ralph.py [codex] [--check]
ARGS = sys.argv[1:]
CHECK_ONLY = "--check" in ARGS
POSITIONAL_ARGS = [arg for arg in ARGS if not arg.startswith("--")]
AGENT = POSITIONAL_ARGS[0] if POSITIONAL_ARGS else "claude"

if AGENT not in {"claude", "codex"}:
    print(f"❌ 不支持的 agent: {AGENT}. 仅支持 claude 或 codex。", file=sys.stderr)
    sys.exit(2)


def build_cmd(prompt: str) -> list[str]:
    """根据 AGENT 配置构建命令"""
    if AGENT == "codex":
        return ["codex", "exec", "--dangerously-bypass-approvals-and-sandbox", prompt]
    return ["claude", "--print", "--dangerously-skip-permissions", prompt]


def build_process_cmd(prompt: str) -> list[str]:
    """通过 script 提供 PTY，确保子进程输出实时显示到控制台"""
    return ["script", "-q", "/dev/null"] + build_cmd(prompt)

# 目录配置
SCRIPT_DIR = Path(__file__).parent.resolve()
PROJECT_ROOT = SCRIPT_DIR.parent.parent
CLAUDE_INSTRUCTION_FILE = SCRIPT_DIR / "CLAUDE.md"
VALIDATOR_INSTRUCTION_FILE = SCRIPT_DIR / "VALIDATOR.md"
PRD_FILE = SCRIPT_DIR / "prd.json"
PROGRESS_FILE = SCRIPT_DIR / "progress.txt"

FORBIDDEN_COMMIT_PREFIXES = (
    ".cache/",
    "logs/",
    "outputs/",
    "reports/",
    "tmp/",
    "temp/",
)
FORBIDDEN_COMMIT_EXACT = {
    ".env",
}
FORBIDDEN_COMMIT_SUFFIXES = (
    ".log",
    ".tmp",
    ".temp",
    ".pyc",
)
FORBIDDEN_COMMIT_PARTS = {
    "__pycache__",
}
AUTO_COMMIT = os.environ.get("RALPH_AUTO_COMMIT", "1").lower() not in {"0", "false", "no"}

# US-002: when the run finishes and all stories are resolved, auto_merge_branch()
# folds the ralph/<name> work branch back into the base branch (the branch the
# run started on, captured by US-001's ensure_work_branch()). The env var
# defaults to "1" (enabled) and the same 0/false/no opt-out as RALPH_AUTO_COMMIT.
# This flag only gates merging; it never has any side effect on git push.
AUTO_MERGE = os.environ.get("RALPH_AUTO_MERGE", "1").lower() not in {"0", "false", "no"}

# Branch lifecycle: BASE_BRANCH captures the branch the run started on (for the
# final --no-ff merge in US-002). WORK_BRANCH is the ralph/<name> branch the
# loop commits to. Both are populated by ensure_work_branch() and left as None
# when prd.json does not declare a ralph/ branchName (bootstrap placeholder).
BASE_BRANCH: str | None = None
WORK_BRANCH: str | None = None
BOOTSTRAP_BRANCH_PLACEHOLDER = "ralph/bootstrap-placeholder"


def run_developer(iteration: int) -> bool:
    """
    调用开发 Agent
    返回值：是否超时
    """
    print(f"\n{'='*64}\n  迭代 {iteration}/{MAX_ITERATIONS}\n{'='*64}")

    if not CLAUDE_INSTRUCTION_FILE.exists():
        print(f"❌ 错误: {CLAUDE_INSTRUCTION_FILE} 不存在")
        return False

    prompt = CLAUDE_INSTRUCTION_FILE.read_text()
    cmd = build_process_cmd(prompt)

    try:
        process = subprocess.Popen(
            cmd,
            cwd=str(PROJECT_ROOT)
        )

        start_time = time.time()

        while True:
            ret_code = process.poll()
            if ret_code is not None:
                print("\n✓ 开发迭代完成")
                return False

            elapsed_time = time.time() - start_time
            if elapsed_time > TIMEOUT_SECONDS:
                print(f"\n⚠️  开发 Agent 超时! 已运行 {int(elapsed_time)} 秒")
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
                print("   进程已终止，将在下一次迭代重试")
                return True

            time.sleep(60)

    except Exception as e:
        print(f"\n❌ 开发 Agent 错误: {e}")
        return False

def run_validator(iteration: int) -> None:
    """
    调用 Validator Agent，由其自行读取 progress.txt 中最后一个 story 进行验证
    """
    print(f"\n{'='*64}\n  验证迭代 {iteration} - Validator 开始工作\n{'='*64}")

    if not VALIDATOR_INSTRUCTION_FILE.exists():
        print(f"⚠️  警告: {VALIDATOR_INSTRUCTION_FILE} 不存在，跳过验证")
        return

    prompt = VALIDATOR_INSTRUCTION_FILE.read_text()
    cmd = build_process_cmd(prompt)

    try:
        process = subprocess.Popen(
            cmd,
            cwd=str(PROJECT_ROOT)
        )

        start_time = time.time()

        while True:
            ret_code = process.poll()
            if ret_code is not None:
                print("\n✓ 验证完成")
                return

            elapsed_time = time.time() - start_time
            if elapsed_time > TIMEOUT_SECONDS * 2:
                print(f"\n⚠️  Validator 超时! 已运行 {int(elapsed_time)} 秒")
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
                print("   Validator 进程已终止，跳过本次验证")
                return

            time.sleep(60)

    except Exception as e:
        print(f"\n❌ Validator 错误: {e}")


def run_git(args: list[str], *, capture: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["git", *args],
        cwd=str(PROJECT_ROOT),
        text=True,
        capture_output=capture,
        check=False,
    )


def _current_branch() -> str | None:
    """Return the current short branch name, or None if detached / git error."""
    result = run_git(["rev-parse", "--abbrev-ref", "HEAD"])
    if result.returncode != 0:
        return None
    name = result.stdout.strip()
    if not name or name == "HEAD":
        return None
    return name


def _branch_exists(branch: str) -> bool:
    result = run_git(["rev-parse", "--verify", "--quiet", f"refs/heads/{branch}"])
    return result.returncode == 0


def ensure_work_branch(check_only: bool = False) -> str | None:
    """
    Create or switch to the ralph/ work branch declared in prd.json.branchName.

    Behavior:
    - Returns the resolved work branch name, or None when branch management is
      skipped (empty branchName, non-ralph/ prefix, or bootstrap placeholder).
    - When branchName is valid, BASE_BRANCH is captured on the first call and
      WORK_BRANCH holds the resolved branch. Both are module-level so US-002's
      auto_merge_branch() can read them later in the same process.
    - In check_only=True (used by --check mode) the function only prints the
      resolved Work branch and Base branch lines and never runs git checkout.
    - In live mode it takes the right one of three actions:
        * already on the work branch  -> reuse, no checkout
        * local branch already exists  -> git checkout <branch>
        * branch does not exist        -> git checkout -b <branch>
      Each path prints: Work branch: <branch> (base: <base>)

    Returns None on skip; the resolved branch name on success.
    """
    global BASE_BRANCH, WORK_BRANCH

    if BASE_BRANCH is None:
        BASE_BRANCH = _current_branch()

    try:
        prd = load_prd()
    except Exception as exc:
        print(f"⚠️  prd.json 读取失败,跳过工作分支管理: {exc}")
        return None

    branch_name = str(prd.get("branchName", "")).strip()
    if not branch_name:
        print("ℹ️  prd.json.branchName 为空,保持当前单分支行为。")
        return None
    if not branch_name.startswith("ralph/"):
        print(f"ℹ️  prd.json.branchName='{branch_name}' 不是 ralph/ 前缀,保持当前单分支行为。")
        return None
    if branch_name == BOOTSTRAP_BRANCH_PLACEHOLDER:
        print(f"ℹ️  prd.json.branchName 是 bootstrap placeholder,保持当前单分支行为。")
        return None

    base = BASE_BRANCH or "<unknown>"
    if check_only:
        WORK_BRANCH = branch_name
        print(f"Work branch: {branch_name} (base: {base})")
        print(f"Base branch: {base}")
        return branch_name

    current = _current_branch()
    if current == branch_name:
        print(f"Work branch: {branch_name} (base: {base})")
        WORK_BRANCH = branch_name
        return branch_name

    if _branch_exists(branch_name):
        result = run_git(["checkout", branch_name], capture=True)
        if result.returncode != 0:
            print(f"❌ git checkout {branch_name} 失败: {result.stderr.strip()}")
            return None
        print(f"Work branch: {branch_name} (base: {base})")
        WORK_BRANCH = branch_name
        return branch_name

    result = run_git(["checkout", "-b", branch_name], capture=True)
    if result.returncode != 0:
        print(f"❌ git checkout -b {branch_name} 失败: {result.stderr.strip()}")
        return None
    print(f"Work branch: {branch_name} (base: {base})")
    WORK_BRANCH = branch_name
    return branch_name


def git_status_paths() -> set[str]:
    result = run_git(["status", "--porcelain=v1", "--untracked-files=all"])
    if result.returncode != 0:
        print(f"⚠️  git status 失败: {result.stderr.strip()}")
        return set()

    paths: set[str] = set()
    for line in result.stdout.splitlines():
        if not line:
            continue
        path_text = line[3:]
        if " -> " in path_text:
            path_text = path_text.split(" -> ", 1)[1]
        paths.add(path_text.strip())
    return paths


def is_forbidden_commit_path(path_text: str) -> bool:
    normalized = path_text.replace("\\", "/")
    if normalized in FORBIDDEN_COMMIT_EXACT:
        return True
    if normalized.startswith(".env."):
        return True
    if normalized.endswith(FORBIDDEN_COMMIT_SUFFIXES):
        return True
    if normalized.startswith(FORBIDDEN_COMMIT_PREFIXES):
        return True
    return any(part in FORBIDDEN_COMMIT_PARTS for part in Path(normalized).parts)


def load_prd() -> dict:
    return json.loads(PRD_FILE.read_text())


def get_story(story_id: str | None) -> dict | None:
    if not story_id:
        return None
    try:
        prd = load_prd()
    except Exception:
        return None
    for story in prd.get("userStories", []):
        if story.get("id") == story_id:
            return story
    return None


def story_ready_to_commit(story_id: str | None) -> bool:
    story = get_story(story_id)
    if not story:
        print("⚠️  未找到当前 story，跳过自动提交。")
        return False
    if story.get("blocked", False):
        print(f"⚠️  {story_id} 已 blocked，跳过自动提交。")
        return False
    if not story.get("passes", False):
        print(f"⚠️  {story_id} 未通过 Validator，跳过自动提交。")
        return False
    if str(story.get("notes", "")).strip():
        print(f"⚠️  {story_id} 仍有 Validator notes，跳过自动提交。")
        return False
    return True


def auto_commit_story(story_id: str | None, baseline_paths: set[str]) -> None:
    if not AUTO_COMMIT:
        print("ℹ️  RALPH_AUTO_COMMIT=0，跳过自动提交。")
        return
    if not story_ready_to_commit(story_id):
        return

    current_paths = git_status_paths()
    candidate_paths = sorted(current_paths - baseline_paths)
    safe_paths = [path for path in candidate_paths if not is_forbidden_commit_path(path)]
    skipped_paths = [path for path in candidate_paths if is_forbidden_commit_path(path)]

    if skipped_paths:
        print("⚠️  以下路径被自动提交安全规则排除:")
        for path in skipped_paths:
            print(f"   - {path}")

    if not safe_paths:
        print("ℹ️  没有可安全提交的新变更。")
        return

    add_result = run_git(["add", "--", *safe_paths])
    if add_result.returncode != 0:
        print(f"❌ git add 失败: {add_result.stderr.strip()}")
        return

    staged_result = run_git(["diff", "--cached", "--name-only"])
    staged_paths = [line.strip() for line in staged_result.stdout.splitlines() if line.strip()]
    forbidden_staged = [path for path in staged_paths if is_forbidden_commit_path(path) or path in baseline_paths]
    if forbidden_staged:
        print("❌ staged 区含禁止或启动前已 dirty 的路径，取消本次自动提交:")
        for path in forbidden_staged:
            print(f"   - {path}")
        restore_result = run_git(["restore", "--staged", "--", *staged_paths])
        if restore_result.returncode != 0:
            print(f"⚠️  git restore --staged 失败: {restore_result.stderr.strip()}")
        return

    story = get_story(story_id) or {}
    title = str(story.get("title", "Ralph story")).strip()
    commit_message = f"feat: {story_id} - {title}"
    commit_result = run_git(["commit", "-m", commit_message], capture=True)
    if commit_result.returncode != 0:
        print(f"❌ git commit 失败: {commit_result.stderr.strip()}")
        return

    print(commit_result.stdout.strip())
    print(f"✅ 已自动提交: {commit_message}")


def check_installation() -> int:
    required_files = [
        CLAUDE_INSTRUCTION_FILE,
        VALIDATOR_INSTRUCTION_FILE,
        PRD_FILE,
        PROGRESS_FILE,
        PROJECT_ROOT / "AGENTS.md",
        PROJECT_ROOT / "skills/chatlog-http-cli/SKILL.md",
        PROJECT_ROOT / "feature_list.json",
        PROJECT_ROOT / "progress.md",
        PROJECT_ROOT / "session-handoff.md",
    ]
    missing = [path for path in required_files if not path.exists()]
    if missing:
        print("❌ Ralph 安装检查失败，缺少文件:")
        for path in missing:
            print(f"   - {path.relative_to(PROJECT_ROOT)}")
        return 1

    try:
        load_prd()
    except Exception as exc:
        print(f"❌ prd.json 不是合法 JSON: {exc}")
        return 1

    dirty_paths = sorted(git_status_paths())
    print("✅ Ralph 安装检查通过")
    print(f"Project root: {PROJECT_ROOT}")
    print(f"Agent: {AGENT}")
    print(f"Auto commit: {'enabled' if AUTO_COMMIT else 'disabled'}")
    if dirty_paths:
        print("Current dirty paths:")
        for path in dirty_paths:
            print(f"  - {path}")
    # US-001 AC#4: --check mode only reads the resolved branch state, never
    # runs any checkout. The two lines are the entire check-mode output.
    ensure_work_branch(check_only=True)
    return 0


def get_current_story_id() -> str | None:
    """返回 prd.json 中第一个 passes=False 且 blocked=False 的 story ID"""
    try:
        prd = json.loads(PRD_FILE.read_text())
        for story in prd.get("userStories", []):
            if not story.get("passes", False) and not story.get("blocked", False):
                return story.get("id")
    except Exception:
        pass
    return None


def all_stories_resolved() -> bool:
    """
    检查 prd.json，判断是否所有 story 都已完成或被 blocked
    """
    try:
        prd = json.loads(PRD_FILE.read_text())
        stories = prd.get("userStories", [])
        for story in stories:
            passes = story.get("passes", False)
            blocked = story.get("blocked", False)
            if not passes and not blocked:
                return False
        return True
    except Exception as e:
        print(f"⚠️  读取 prd.json 失败: {e}")
        return False


def _collect_resolved_story_ids() -> list[str]:
    """Return all story IDs in prd.json order (used for the merge commit message)."""
    try:
        prd = json.loads(PRD_FILE.read_text())
        return [str(s.get("id", "")) for s in prd.get("userStories", []) if s.get("id")]
    except Exception:
        return []


def _list_blocked_story_ids() -> list[str]:
    """Return IDs of all blocked stories; empty list when no story is blocked."""
    try:
        prd = json.loads(PRD_FILE.read_text())
        return [
            str(s.get("id", ""))
            for s in prd.get("userStories", [])
            if s.get("blocked", False) and s.get("id")
        ]
    except Exception:
        return []


def auto_merge_branch() -> bool:
    """
    US-002: fold the ralph/<name> work branch back into the base branch when
    every story has passed validation and no story is blocked.

    Returns True on successful merge, False when the merge is intentionally
    skipped (no branch management, blocked story, or AUTO_MERGE disabled).
    Raises SystemExit(1) when a merge is attempted but fails with conflicts
    (after a clean `git merge --abort` + checkout back to the work branch).

    The function never calls `git push`. RALPH_AUTO_MERGE=0/false/no is the
    only documented opt-out; the disabled state prints a clear notice and
    leaves the work branch untouched.
    """
    global BASE_BRANCH, WORK_BRANCH

    if WORK_BRANCH is None:
        print("ℹ️  工作分支未启用(ensure_work_branch 返回 None),跳过自动合并。")
        return False

    blocked_ids = _list_blocked_story_ids()
    if blocked_ids:
        print("⚠️  以下 story 已 BLOCKED,跳过自动合并(防止半成品合并):")
        for sid in blocked_ids:
            print(f"   - {sid}")
        return False

    if not AUTO_MERGE:
        print("ℹ️  RALPH_AUTO_MERGE=0,跳过自动合并。工作分支保留未合并状态。")
        return False

    if BASE_BRANCH is None:
        print("⚠️  启动时未捕获 base 分支,无法执行自动合并。")
        return False

    story_ids = _collect_resolved_story_ids()
    story_count = len(story_ids)
    branch_name = prd_branch_name() or WORK_BRANCH
    merge_message = f"merge: {branch_name} ({story_count} stories)"

    print(f"🚀 自动合并 {WORK_BRANCH} -> {BASE_BRANCH}")
    print(f"   commit message: {merge_message}")

    checkout_result = run_git(["checkout", BASE_BRANCH], capture=True)
    if checkout_result.returncode != 0:
        print(f"❌ git checkout {BASE_BRANCH} 失败: {checkout_result.stderr.strip()}")
        # Best-effort: try to return to the work branch so the run can still
        # inspect state without leaving the user in the wrong branch.
        run_git(["checkout", WORK_BRANCH], capture=True)
        return False

    merge_result = run_git(
        ["merge", "--no-ff", WORK_BRANCH, "-m", merge_message],
        capture=True,
    )
    if merge_result.returncode != 0:
        # Conflict path: abort the in-progress merge, leave base branch
        # untouched, switch back to the work branch, surface the conflict
        # file list, and exit non-zero so callers can observe the failure.
        print(f"❌ git merge 失败(冲突): {merge_result.stderr.strip()}")
        abort_result = run_git(["merge", "--abort"], capture=True)
        if abort_result.returncode != 0:
            print(f"⚠️  git merge --abort 失败: {abort_result.stderr.strip()}")
        switch_back = run_git(["checkout", WORK_BRANCH], capture=True)
        if switch_back.returncode != 0:
            print(f"⚠️  切回 {WORK_BRANCH} 失败: {switch_back.stderr.strip()}")
        # Recompute conflict list after abort so the diff reflects the base
        # branch state, not the half-applied merge.
        conflict_result = run_git(["diff", "--name-only", BASE_BRANCH, WORK_BRANCH], capture=True)
        if conflict_result.returncode == 0 and conflict_result.stdout.strip():
            print("冲突文件列表(base vs work-branch 差异):")
            for path in conflict_result.stdout.splitlines():
                print(f"   - {path.strip()}")
        else:
            print(f"⚠️  无法获取冲突文件列表(git diff exit {conflict_result.returncode})")
        sys.exit(1)

    # Success: capture the new merge commit hash and confirm the work branch
    # still exists (we never delete it, per AC#2).
    log_result = run_git(["log", "-1", "--format=%H"], capture=True)
    merge_commit = log_result.stdout.strip() if log_result.returncode == 0 else "<unknown>"
    print(f"✅ 合并完成: commit {merge_commit}")
    if _branch_exists(WORK_BRANCH):
        print(f"ℹ️  工作分支 {WORK_BRANCH} 仍保留(未删除)。")
        print(f"   手动删除命令: git branch -d {WORK_BRANCH}")
    else:
        print(f"ℹ️  工作分支 {WORK_BRANCH} 已被外部删除,跳过保留提示。")
    return True


def prd_branch_name() -> str | None:
    """Return the prd.json branchName field when it is a valid ralph/ name."""
    try:
        prd = json.loads(PRD_FILE.read_text())
    except Exception:
        return None
    raw = str(prd.get("branchName", "")).strip()
    if not raw or not raw.startswith("ralph/"):
        return None
    return raw


def format_duration(seconds: float) -> str:
    """将秒数格式化为易读的时间字符串"""
    h = int(seconds // 3600)
    m = int((seconds % 3600) // 60)
    s = int(seconds % 60)
    if h > 0:
        return f"{h}小时 {m}分钟 {s}秒"
    elif m > 0:
        return f"{m}分钟 {s}秒"
    else:
        return f"{s}秒"


def main():
    """主函数"""
    if CHECK_ONLY:
        sys.exit(check_installation())

    print(f"启动 Ralph - 最大迭代次数: {MAX_ITERATIONS}")
    total_start_time = time.time()
    startup_dirty_paths = git_status_paths()
    if startup_dirty_paths:
        print("启动前已有 dirty 路径，Ralph 自动提交会排除这些路径:")
        for path in sorted(startup_dirty_paths):
            print(f"  - {path}")

    # US-001 AC#2: capture base branch and switch to the ralph/ work branch
    # before the iteration loop. ensure_work_branch() prints the same
    # "Work branch: <branch> (base: <base>)" line for all three paths
    # (already on branch / exists / newly created). When prd.json does not
    # declare a ralph/ branchName this is a no-op that preserves the
    # single-branch behavior.
    ensure_work_branch()

    dashboard.start(max_iterations=MAX_ITERATIONS)

    for i in range(1, MAX_ITERATIONS + 1):
        try:
            # 第一步：调用开发 Agent
            current_story = get_current_story_id()
            iteration_baseline = git_status_paths()
            dashboard.set_state(iteration=i, phase="developing", current_story=current_story)
            timed_out = run_developer(i)

            # 开发 Agent 超时，跳过 Validator，直接进入下一次迭代重试
            if timed_out:
                dashboard.set_state(phase="idle")
                print("⏭️  开发 Agent 超时，跳过验证，下一次迭代继续开发...")
                time.sleep(2)
                continue

            # 第二步：开发 Agent 正常完成，调用 Validator Agent
            dashboard.set_state(phase="validating")
            run_validator(i)
            auto_commit_story(current_story, iteration_baseline)

            # 第三步：检查是否全部完成（passes:true 或 blocked:true）
            dashboard.set_state(phase="idle")
            if all_stories_resolved():
                dashboard.set_state(phase="done")
                elapsed = time.time() - total_start_time
                print("✅ 所有任务已完成或已标记为 BLOCKED!")
                print(f"⏱️  总运行时间: {format_duration(elapsed)}")
                # US-002: fold the ralph/<name> work branch back into the base
                # branch when every story has passed and no story is blocked.
                # auto_merge_branch() handles its own skip/disabled/abort paths
                # and only exits non-zero on an actual conflict.
                auto_merge_branch()
                sys.exit(0)

        except KeyboardInterrupt:
            elapsed = time.time() - total_start_time
            print(f"\n\n⚠️  用户中断")
            print(f"⏱️  总运行时间: {format_duration(elapsed)}")
            sys.exit(130)

    elapsed = time.time() - total_start_time
    print(f"\n已达到最大迭代次数 ({MAX_ITERATIONS})")
    print(f"⏱️  总运行时间: {format_duration(elapsed)}")
    sys.exit(1)


if __name__ == "__main__":
    main()
