class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.26"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.26/xissue-v0.2.26-darwin-arm64.tar.gz"
      sha256 "2c77aba2052c41ede35b1a83229dad1d1541f8d0821d39010a69b6b5f37a8783"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.26/xissue-v0.2.26-darwin-amd64.tar.gz"
      sha256 "a43088edd32d370e431e37aa08e856b745fbd5550dd2f73d2d84ac1fc0248fd3"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.26/xissue-v0.2.26-linux-arm64.tar.gz"
      sha256 "cc1e64500eee70d5b00f5aa9643219f2387999918c2d82dffd9eee47d636b219"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.26/xissue-v0.2.26-linux-amd64.tar.gz"
      sha256 "2dfef0993953470229ed381bc4d1a53051a37bd3fce675cb594db8eb0e772467"
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
