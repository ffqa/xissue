class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.8"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.8/xissue-v0.2.8-darwin-arm64.tar.gz"
      sha256 "47ce52af8b267d02639b04016f1f10218200448ed5ccc6adb640fb2fd244418b"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.8/xissue-v0.2.8-darwin-amd64.tar.gz"
      sha256 "3b8ab449b7f005b935d3fa7ba39d46cca211100ccd22399661bf104f66d628cf"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.8/xissue-v0.2.8-linux-arm64.tar.gz"
      sha256 "4ca99250f65cd257f96cdd0e3d60815b75ecadc35d7d79e3dda125f7db590deb"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.8/xissue-v0.2.8-linux-amd64.tar.gz"
      sha256 "0e76459baf5c5624fb9b30d945519fc17d3d7423fb19d8baf27b0fcef8289626"
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
