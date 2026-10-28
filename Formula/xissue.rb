class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.41"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.41/xissue-v0.2.41-darwin-arm64.tar.gz"
      sha256 "f8f366cd21b72e014f5097e27f3f64f3fdfa543d0c369175071cacc03fb7b197"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.41/xissue-v0.2.41-darwin-amd64.tar.gz"
      sha256 "0808d909ccb102275effb78f2a186289d40ab84b88da9c897a031b668fd5384f"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.41/xissue-v0.2.41-linux-arm64.tar.gz"
      sha256 "c74efc98c157cf0ced222b47430beb5d960907b02ad5361131e099d947793b6a"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.41/xissue-v0.2.41-linux-amd64.tar.gz"
      sha256 "cbe57fbb27c5e05722c24ef04ee5414574cebc9cfecb16305c62f3b26fa48b28"
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
