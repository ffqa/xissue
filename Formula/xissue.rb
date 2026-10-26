class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.27"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.27/xissue-v0.2.27-darwin-arm64.tar.gz"
      sha256 "35676b6034e6006934600c4acae5c7fceb168f368cde15f860d87a235b5b9912"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.27/xissue-v0.2.27-darwin-amd64.tar.gz"
      sha256 "82552c14c9209ee59bbd9d0001589278b838882d0ecb4f77913b8cf505237fb0"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.27/xissue-v0.2.27-linux-arm64.tar.gz"
      sha256 "963907319046f121bf86599e3a6cf8aff560589efbb33909bd644852ada9772c"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.27/xissue-v0.2.27-linux-amd64.tar.gz"
      sha256 "5d3ee5fa327f4a3ceb6bdcdc3de993d108edc958eff59be07fe3c23b33277d3c"
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
