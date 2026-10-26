class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.32"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.32/xissue-v0.2.32-darwin-arm64.tar.gz"
      sha256 "62dfa1a7757b65a326255dfe767506349f949f77370539f28ebc57d39ae0e889"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.32/xissue-v0.2.32-darwin-amd64.tar.gz"
      sha256 "6b64919ec363752e9d12be9d60ce6d34d4b2f984934c31edfcc1f6873fca4a93"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.32/xissue-v0.2.32-linux-arm64.tar.gz"
      sha256 "23380daf45ff4f6f1d3dfdf18e0d67d57ea49f3e988581503dbb33368cd02b8f"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.32/xissue-v0.2.32-linux-amd64.tar.gz"
      sha256 "eb17c671a8c6b009cbc554998b5f762e8814251ad32738a09c451daf3019c99c"
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
