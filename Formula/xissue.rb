class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.0/xissue-v0.8.0-darwin-arm64.tar.gz"
      sha256 "e420406f85634f96df408154ce6a6a973e256a12ed62d60642ec812e27811cd9"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.0/xissue-v0.8.0-darwin-amd64.tar.gz"
      sha256 "fc17362637de7a665c8d1980125691c978dcde2c02ff4a4f54a0f597130c481b"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.0/xissue-v0.8.0-linux-arm64.tar.gz"
      sha256 "021c8ebff2eee7c50cac121d8334b389fd94556ab82608123ddf917342f29c66"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.0/xissue-v0.8.0-linux-amd64.tar.gz"
      sha256 "2cd9fc059f30e7d45e2c4b75bb0fdbfc0c7c34340523bbe6fd67a5b5e3f252a0"
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
