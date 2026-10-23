class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.20"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.20/xissue-v0.2.20-darwin-arm64.tar.gz"
      sha256 "eb710a30c99a35876fbb181b1cc680ff90e85486e2372565bbfe6a1f3b0b0e92"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.20/xissue-v0.2.20-darwin-amd64.tar.gz"
      sha256 "475b0c2e481fca030c6deeaa661576aa600e046a1df9770a12c2a887f20613fe"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.20/xissue-v0.2.20-linux-arm64.tar.gz"
      sha256 "ccdc0b798cb079df0772d099dc56d32e3fe5d4a7dd5104036d41fd1c64b3c7ab"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.20/xissue-v0.2.20-linux-amd64.tar.gz"
      sha256 "54546df8fc22062383151e25057bdb177c3278d29169e1748e3c7e9ddf4101df"
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
