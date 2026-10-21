class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.14"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.14/xissue-v0.2.14-darwin-arm64.tar.gz"
      sha256 "3669e971b6ffaaca477b824fb08b4b4df69063d28868ed54a60b04e44866f24c"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.14/xissue-v0.2.14-darwin-amd64.tar.gz"
      sha256 "62f806851e89d36627bbcacc2ecf3b3c0d50a80779a89a819eb7202b12e7dce6"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.14/xissue-v0.2.14-linux-arm64.tar.gz"
      sha256 "900454ade2cd7237d5cb08457ffada2e5ae600fd7d7ddb26a8eb13c5e5a9c43d"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.14/xissue-v0.2.14-linux-amd64.tar.gz"
      sha256 "3c8589e2b04b7125e914516f15172fe1fb76251dcb863089673e9f4600783369"
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
