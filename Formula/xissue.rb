class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.5.7"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.7/xissue-v0.5.7-darwin-arm64.tar.gz"
      sha256 "65cd57f2795536eb56b3563e231b8a3ad9f8eeb1d5a446e9c7c8a353eab5c535"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.7/xissue-v0.5.7-darwin-amd64.tar.gz"
      sha256 "a93f29be21c0af597cb653bd1a31fcdb4d8f925329edde420a2d3869e1294472"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.7/xissue-v0.5.7-linux-arm64.tar.gz"
      sha256 "efb1483c52c4171a9d3c16725d20bced99488af7418c1a3a7ca8e84ea5e2cbf7"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.7/xissue-v0.5.7-linux-amd64.tar.gz"
      sha256 "561e582d5574bbc6050b14716ba5f4c3f7bb6a5cd5f94568505a9cf84915e853"
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
