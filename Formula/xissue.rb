class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.5.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.1/xissue-v0.5.1-darwin-arm64.tar.gz"
      sha256 "f1a4021ba341991dc591edbc4ac303f5711c50d8b0920a7b14fffb8b7bdb1b38"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.1/xissue-v0.5.1-darwin-amd64.tar.gz"
      sha256 "b57760f3b97bf787f70b65a7fb30c0769c8e7d1b6645391c765d0f09c7979d46"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.1/xissue-v0.5.1-linux-arm64.tar.gz"
      sha256 "1aef5e6265360543c8450610009f3bbe65f2835162cb78ddd33e18b3a73d1b8e"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.1/xissue-v0.5.1-linux-amd64.tar.gz"
      sha256 "08961d9077f3b56a68ccc2f42a7eaacd2972309f335f81023088be9c60fbfd46"
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
