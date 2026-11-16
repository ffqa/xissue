class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.13"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.13/xissue-v0.8.13-darwin-arm64.tar.gz"
      sha256 "a5dc3e148e411aa8192065113338aeeddd160d4b2dc4c1d3551207828dfa15bf"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.13/xissue-v0.8.13-darwin-amd64.tar.gz"
      sha256 "09692ea041844ad94773de2ecd5d689cad1bd76802a9f7bfef90f158005c0e5e"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.13/xissue-v0.8.13-linux-arm64.tar.gz"
      sha256 "1b1c202fa35a6d29cefbbe97a57b1f57b3e08ea3ed785756bb7f1c63bc5c4db8"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.13/xissue-v0.8.13-linux-amd64.tar.gz"
      sha256 "5da09b637dfab2e5d0d2261fde0ffe62523381b01c3fea6d9e5095d6f3008c10"
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
