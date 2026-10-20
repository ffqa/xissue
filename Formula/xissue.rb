class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.11"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.11/xissue-v0.2.11-darwin-arm64.tar.gz"
      sha256 "81203ae74ff27539f56ba1f3c35979180c1f2f3e8554e2623154a724d9e19c68"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.11/xissue-v0.2.11-darwin-amd64.tar.gz"
      sha256 "b50ecf64da38c1e280ca734360525fca16b8974233f497c4c96caea6467187a4"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.11/xissue-v0.2.11-linux-arm64.tar.gz"
      sha256 "50ed8dd62fefe362e00639dc7446b0efa7f9c93fa3b218bee33e97dd5f149b52"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.11/xissue-v0.2.11-linux-amd64.tar.gz"
      sha256 "aaffba1e5037cbc72048e262a175aec952777a122ad5db829e690b9d2676a1df"
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
