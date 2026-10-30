class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.6.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.6.0/xissue-v0.6.0-darwin-arm64.tar.gz"
      sha256 "f89a2932a631b3863f73f80acc6fc7c6aaeb65d923ffc4fd44a8334f8d1b5775"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.6.0/xissue-v0.6.0-darwin-amd64.tar.gz"
      sha256 "a494c3da2058e45edc6910f8eef5254d3e9ab53f75be4ef42457c257954d9cc7"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.6.0/xissue-v0.6.0-linux-arm64.tar.gz"
      sha256 "25f3d6579ab78b61020d8447ca53ab14055444a43d2ca23fe8d128ed7f7ed1ab"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.6.0/xissue-v0.6.0-linux-amd64.tar.gz"
      sha256 "b76444f2db7f434b0b4ed6c21194cd05c6407a11554a245c888bfbef85b74477"
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
