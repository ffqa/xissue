#!/usr/bin/env bash
# xIssue Homebrew Formula 发布脚本
# 用法：
#   bash scripts/brew-publish.sh                     # 自动检测最新版本，生成 Formula 并推送到 ffqa/homebrew-tap
#   bash scripts/brew-publish.sh v0.2.7              # 指定版本
#   bash scripts/brew-publish.sh --formula-only      # 仅在本地生成 Formula/xissue.rb，不推送到 Tap
set -euo pipefail

GH_USER="ffqa"
MAIN_REPO="xissue"
TAP_REPO="homebrew-tap"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION=""
FORMULA_ONLY=0

for arg in "$@"; do
  case "$arg" in
    --formula-only) FORMULA_ONLY=1 ;;
    v*) VERSION="${arg#v}" ;;
    [0-9]*) VERSION="$arg" ;;
    *) echo "未知参数: $arg" >&2; exit 1 ;;
  esac
done

if [[ -z "$VERSION" ]]; then
  # 优先从 git tag 或 GitHub release 获取
  VERSION="$(git describe --tags --abbrev=0 2>/dev/null || echo "")"
  VERSION="${VERSION#v}"
fi

if [[ -z "$VERSION" ]]; then
  echo "无法确定版本号，请指定版本，例如: bash scripts/brew-publish.sh 0.2.7" >&2
  exit 1
fi

TAG="v${VERSION}"
BASE_URL="https://github.com/${GH_USER}/${MAIN_REPO}/releases/download/${TAG}"
SUMS_URL="${BASE_URL}/SHA256SUMS.txt"

echo "正在从 ${SUMS_URL} 获取校验和..."
SUMS_CONTENT="$(curl -fsSL "$SUMS_URL" || true)"

if [[ -z "$SUMS_CONTENT" ]]; then
  echo "警告: 无法下载 ${SUMS_URL}，尝试从本地构建产物或直接计算..." >&2
fi

get_sha() {
  local asset="$1"
  local sha=""
  if [[ -n "$SUMS_CONTENT" ]]; then
    sha="$(echo "$SUMS_CONTENT" | grep "$asset" | awk '{print $1}' || true)"
  fi
  if [[ -z "$sha" && -f "dist/release/$asset" ]]; then
    sha="$(shasum -a 256 "dist/release/$asset" | awk '{print $1}')"
  fi
  if [[ -z "$sha" ]]; then
    echo "正在下载 ${BASE_URL}/${asset} 计算 sha256..." >&2
    sha="$(curl -fsSL "${BASE_URL}/${asset}" | shasum -a 256 | awk '{print $1}' || true)"
  fi
  echo "$sha"
}

DARWIN_ARM64_SHA="$(get_sha "xissue-${TAG}-darwin-arm64.tar.gz")"
DARWIN_AMD64_SHA="$(get_sha "xissue-${TAG}-darwin-amd64.tar.gz")"
LINUX_ARM64_SHA="$(get_sha "xissue-${TAG}-linux-arm64.tar.gz")"
LINUX_AMD64_SHA="$(get_sha "xissue-${TAG}-linux-amd64.tar.gz")"

echo "版本: $VERSION"
echo "  darwin-arm64: $DARWIN_ARM64_SHA"
echo "  darwin-amd64: $DARWIN_AMD64_SHA"
echo "  linux-arm64:  $LINUX_ARM64_SHA"
echo "  linux-amd64:  $LINUX_AMD64_SHA"

mkdir -p Formula
cat <<EOF > Formula/xissue.rb
class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/${GH_USER}/${MAIN_REPO}"
  version "${VERSION}"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "${BASE_URL}/xissue-${TAG}-darwin-arm64.tar.gz"
      sha256 "${DARWIN_ARM64_SHA}"
    else
      url "${BASE_URL}/xissue-${TAG}-darwin-amd64.tar.gz"
      sha256 "${DARWIN_AMD64_SHA}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "${BASE_URL}/xissue-${TAG}-linux-arm64.tar.gz"
      sha256 "${LINUX_ARM64_SHA}"
    else
      url "${BASE_URL}/xissue-${TAG}-linux-amd64.tar.gz"
      sha256 "${LINUX_AMD64_SHA}"
    end
  end

  def install
    bin.install "xissue"
    (pkgshare/"scripts").install Dir["scripts/*"] if Dir.exist?("scripts")
    (pkgshare/"deploy").install Dir["deploy/*"] if Dir.exist?("deploy")
  end

  service do
    run [opt_bin/"xissue"]
    keep_alive true
    working_dir Dir.home
    environment_variables XISSUE_SERVICE: "1"
    log_path var/"log/xissue.log"
    error_log_path var/"log/xissue.log"
  end

  def caveats
    <<~EOS
      启动后台常驻服务方式（二选一）：
      1. Homebrew 服务管理:
         brew services start ffqa/tap/xissue
      2. xIssue 自管服务 (推荐，开机自启):
         xissue service install
    EOS
  end

  test do
    assert_match "xissue", shell_output("#{bin}/xissue version")
  end
end
EOF

echo "✓ 已生成 Formula/xissue.rb"

if [[ "$FORMULA_ONLY" == "1" ]]; then
  exit 0
fi

echo "正在将 Formula 发布到 ${GH_USER}/${TAP_REPO}..."
TMP_TAP="$(mktemp -d)"
trap 'rm -rf "$TMP_TAP"' EXIT

if command -v gh >/dev/null 2>&1; then
  gh repo clone "${GH_USER}/${TAP_REPO}" "$TMP_TAP" -- --depth=1
else
  git clone "https://github.com/${GH_USER}/${TAP_REPO}.git" "$TMP_TAP" --depth=1
fi

mkdir -p "$TMP_TAP/Formula"
cp Formula/xissue.rb "$TMP_TAP/Formula/xissue.rb"

cd "$TMP_TAP"
if ! git config user.name >/dev/null 2>&1; then
  git config user.name "github-actions[bot]"
fi
if ! git config user.email >/dev/null 2>&1; then
  git config user.email "github-actions[bot]@users.noreply.github.com"
fi
if [[ -n "${GH_TOKEN:-}" ]]; then
  git remote set-url origin "https://x-access-token:${GH_TOKEN}@github.com/${GH_USER}/${TAP_REPO}.git"
fi

git add Formula/xissue.rb
if git diff --cached --quiet; then
  echo "Tap 中的 Formula 无变化，跳过提交。"
else
  git commit -m "feat(xissue): release ${TAG}"
  git push origin main
  echo "✓ 成功推送到 ${GH_USER}/${TAP_REPO}！"
fi

echo
echo "安装方式："
echo "  brew tap ${GH_USER}/tap"
echo "  brew install ${GH_USER}/tap/xissue"
echo "  brew services start ${GH_USER}/tap/xissue"
