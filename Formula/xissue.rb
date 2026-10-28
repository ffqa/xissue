class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.43"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.43/xissue-v0.2.43-darwin-arm64.tar.gz"
      sha256 "d598908fb0ce334e3df8a27d7e422bd9d82bb33e201e60ed452fc45bb63c462e"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.43/xissue-v0.2.43-darwin-amd64.tar.gz"
      sha256 "1b55ab397387aec60ac73c04ffe4a22718a56010a28d8a2df9d0cb7c974f0a49"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.43/xissue-v0.2.43-linux-arm64.tar.gz"
      sha256 "8a58695e0dcd9b4bcf4a7155950d45c26ad2bb3fb83fdd9179a860e1e3588ce2"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.43/xissue-v0.2.43-linux-amd64.tar.gz"
      sha256 "48ca2d4b52d5f68d2c650f536d5326f81c1e291bf85a3faad200a834f3f3cc89"
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
