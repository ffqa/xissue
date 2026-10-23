class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.21"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.21/xissue-v0.2.21-darwin-arm64.tar.gz"
      sha256 "98fb3f6aa16bf84864646d846e49647a97a0b7d7846f6a803beec1167f4dc2c6"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.21/xissue-v0.2.21-darwin-amd64.tar.gz"
      sha256 "a5a1569693f163e74fa87e75cf9afb89f3069091d5131e3f3d1635210025afaa"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.21/xissue-v0.2.21-linux-arm64.tar.gz"
      sha256 "096aaed047b1240e00cfceb3e4c083227d2d397ba534d603aee3c7758cb483f2"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.21/xissue-v0.2.21-linux-amd64.tar.gz"
      sha256 "ce968af6ea3234a3cbd3544d47112e9567d7fe4619e79a881a9d5972eba18f00"
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
