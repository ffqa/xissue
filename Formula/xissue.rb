class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.8"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.8/xissue-v0.8.8-darwin-arm64.tar.gz"
      sha256 "dfd47f8eb23d4fc8e00978dbf9c3743492669d254ddca7b7e5eb8fdd26a247a5"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.8/xissue-v0.8.8-darwin-amd64.tar.gz"
      sha256 "230b8ff171fd59f7067902435bfd6a0519ebb4ed4f2b556ff4f43074f81196c1"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.8/xissue-v0.8.8-linux-arm64.tar.gz"
      sha256 "698d48218a34ba988fe5e2c3ae5fb601eaa5f0b8e38cb509a7a21684c081cc86"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.8/xissue-v0.8.8-linux-amd64.tar.gz"
      sha256 "6e0c32763dea28614d23c0d43b847d219a69e0c1123944010d50d2aaf6a6facb"
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
