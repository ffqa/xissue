class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.10"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.10/xissue-v0.8.10-darwin-arm64.tar.gz"
      sha256 "c74feade5bc2c99c6f10ed2a3beb2bbcc5052ae474601b77ed48a26fc3a63ef2"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.10/xissue-v0.8.10-darwin-amd64.tar.gz"
      sha256 "81ba968f57569ec7c393fc54bed782118ab81ed1d49d4a28687708b3ce7b4bb4"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.10/xissue-v0.8.10-linux-arm64.tar.gz"
      sha256 "fe5d914689dc84460e307a0c9fe32017210fae4126eb16f48126a8fb35395d64"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.10/xissue-v0.8.10-linux-amd64.tar.gz"
      sha256 "2599865e5e518695ce763ba546c9d25374817f529199256d2a14242580e84514"
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
