class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.18"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.18/xissue-v0.2.18-darwin-arm64.tar.gz"
      sha256 "d19db7d02aeec1be3c0d7ac151fec24711accb4937c5215d87b604d266216bda"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.18/xissue-v0.2.18-darwin-amd64.tar.gz"
      sha256 "e23017e658f4c13eda3fe9c22a286f6296224c8ff21e5eb1aa42681cdf79d52f"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.18/xissue-v0.2.18-linux-arm64.tar.gz"
      sha256 "4cae145e7c8e20c9a446105006634695157daa6a29c8096fb48b9ee4b274bfd3"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.18/xissue-v0.2.18-linux-amd64.tar.gz"
      sha256 "31e79121fbf071e1a257a313985cb85c5972f3e54b611f3306b01d43b5ad015b"
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
