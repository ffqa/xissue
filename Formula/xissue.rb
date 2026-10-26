class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.29"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.29/xissue-v0.2.29-darwin-arm64.tar.gz"
      sha256 "3ee9692533df88897e9e46b3910cf4e38a0a9e99e2a744aba867e16f513ae943"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.29/xissue-v0.2.29-darwin-amd64.tar.gz"
      sha256 "a8921e2bd71a19a1b7855f183e2408ba9477e37da0d7175e6f1c0443da3a2cf4"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.29/xissue-v0.2.29-linux-arm64.tar.gz"
      sha256 "c12141162593ccf71d3bafe8e4f1a17628f1541ca15b77dcadaeb3d33efa367d"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.29/xissue-v0.2.29-linux-amd64.tar.gz"
      sha256 "6393cc5142a928b8f94380de4e1077e42e8b0973f0d989ef9a43dbb33c0a1980"
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
