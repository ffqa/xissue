class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.11"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.11/xissue-v0.8.11-darwin-arm64.tar.gz"
      sha256 "3156afeb422affef02f505f5e9c8cec48d5f75c026a298222808493d7b74a1ba"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.11/xissue-v0.8.11-darwin-amd64.tar.gz"
      sha256 "6da64540393b891d16e66c41a7a4a4bbba60ef9a177ace7b4370f0081129ba4c"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.11/xissue-v0.8.11-linux-arm64.tar.gz"
      sha256 "69c8317dbf8655c27b5bc78fc5802db88f7cd39c6e21ff6d2c0ddf4e20e6564b"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.11/xissue-v0.8.11-linux-amd64.tar.gz"
      sha256 "8010cf1ab7a121ce0c1e56e07b11751a3a94f512522512f49697368ccf3a4ae5"
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
