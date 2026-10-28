class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.4.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.4.0/xissue-v0.4.0-darwin-arm64.tar.gz"
      sha256 "7f0fd305fcb198ccf290b106787fecc1cacba6d7382d8a1f23627a711d6b6b24"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.4.0/xissue-v0.4.0-darwin-amd64.tar.gz"
      sha256 "c9763f4ea9c8a7ead315bb43d9d5af583c8b2a6f929da05e02513a7bcd6c660b"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.4.0/xissue-v0.4.0-linux-arm64.tar.gz"
      sha256 "13f784a52b4c4f108115658c8628c4f2db65042f1ed7c5c325a47c4cb58f2c98"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.4.0/xissue-v0.4.0-linux-amd64.tar.gz"
      sha256 "3418d7bf2f0421241d8e633894b0fe5406cea8c970e06aa91fc8c3b19b11047a"
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
