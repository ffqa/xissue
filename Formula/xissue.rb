class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.22"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.22/xissue-v0.2.22-darwin-arm64.tar.gz"
      sha256 "aee2c7e4b964b357c153e3303f4b16820f2505148ec1da299033802afcb64678"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.22/xissue-v0.2.22-darwin-amd64.tar.gz"
      sha256 "e885b34d7f63888dd1255aae8ba0b6dc5b38c01d68f70e627f30b7ed8ed8a36f"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.22/xissue-v0.2.22-linux-arm64.tar.gz"
      sha256 "f40668e0ccefa206a9244b8f70b6b7d435215ee5f7be13a019ff19edc1f028ca"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.22/xissue-v0.2.22-linux-amd64.tar.gz"
      sha256 "83c7b0962e223024d42ede720c665c3a37293dfcd7b2a14a6a697b780c9a1cfc"
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
