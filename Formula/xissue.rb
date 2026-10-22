class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.19"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.19/xissue-v0.2.19-darwin-arm64.tar.gz"
      sha256 "74be56ec25dac437d4b7a851627aabc41247927021726bcc43e6019af9482061"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.19/xissue-v0.2.19-darwin-amd64.tar.gz"
      sha256 "d4288452b6270d6de1e33a15e7f921fb2d2fca27579a984ab3df6f88c3464848"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.19/xissue-v0.2.19-linux-arm64.tar.gz"
      sha256 "00c3da4df0e8678bf80e1950a2ca0579046b2d9fc30e808ec59a74024dfdff12"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.19/xissue-v0.2.19-linux-amd64.tar.gz"
      sha256 "d0895a9dafe52341095e7e4924fdca35d9f4cf7c96ff717fa4e3511118bea22b"
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
