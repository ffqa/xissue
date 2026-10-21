class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.13"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.13/xissue-v0.2.13-darwin-arm64.tar.gz"
      sha256 "c781ea6b2fd5c9ea330bdb889b67fa148c3159e16913b826904e0bef22bd46ce"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.13/xissue-v0.2.13-darwin-amd64.tar.gz"
      sha256 "a2f16248646c56d2df4f8b596a37efc22d525aeeb12660a64e526398a8ae741d"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.13/xissue-v0.2.13-linux-arm64.tar.gz"
      sha256 "81a6fb9736658d147d2b7577393872d5fca164d846b86abeb2be7df667222ecf"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.13/xissue-v0.2.13-linux-amd64.tar.gz"
      sha256 "6fea8beef8cd7f92369b9a1ceb9d2417ca72af47f933a15390ab4078ba0b7d71"
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
