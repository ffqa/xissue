class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.4.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.4.1/xissue-v0.4.1-darwin-arm64.tar.gz"
      sha256 "8b3465955283c696b91a0dcb6d96d0baf423fe5e5e9b4196b1659700326958c3"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.4.1/xissue-v0.4.1-darwin-amd64.tar.gz"
      sha256 "530fe75de1afbf13525981bbea9b5b283d816b7073da7a6e97c09ff87a40e8e9"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.4.1/xissue-v0.4.1-linux-arm64.tar.gz"
      sha256 "2c92bfae2d9fb2aa7b882a184edd4e4a61029c9dc474b54be4efae5c01877829"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.4.1/xissue-v0.4.1-linux-amd64.tar.gz"
      sha256 "67a25471a6de5cc3211911b051e586b396826df00d6c034f00f87b71ecb6c098"
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
