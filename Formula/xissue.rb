class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.11"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.11/xissue-v0.2.11-darwin-arm64.tar.gz"
      sha256 "9d30ccf2f32fddb6b120254954a6ae1038d7d3e428497354635e018f11299ba6"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.11/xissue-v0.2.11-darwin-amd64.tar.gz"
      sha256 "8b831fc6fad303d7a6b16053992019ae09dfc6e7c5aa7fd1a0ee1d1bc2fd8030"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.11/xissue-v0.2.11-linux-arm64.tar.gz"
      sha256 "213a09c489c51b802049dd7f22054858034a85a86a2a9cb63a257478155541ca"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.11/xissue-v0.2.11-linux-amd64.tar.gz"
      sha256 "ad5dcf5bb1e0a452ca7522ba6acf0808d44752d45564e5c9b9cf30a2d4d23a6f"
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
