class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.5.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.0/xissue-v0.5.0-darwin-arm64.tar.gz"
      sha256 "612f4177f5bf2f68cdbd2b1bbcb909024b2e7799725be8701817760111a59ea1"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.0/xissue-v0.5.0-darwin-amd64.tar.gz"
      sha256 "71d9bd58414e50906c2d60354f95e3c7db0f04080c0551d13e50adb0a9ad813e"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.0/xissue-v0.5.0-linux-arm64.tar.gz"
      sha256 "6e30a14e538ef9e9cddbfdde9ec5dd1fa2bce20961ed9155250d674f12da1e99"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.0/xissue-v0.5.0-linux-amd64.tar.gz"
      sha256 "faf8795f39ed64e54a2e3c0118d218ba4b3944be9c53e5e7ac1b23121b447a96"
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
