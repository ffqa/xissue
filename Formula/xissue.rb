class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.24"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.24/xissue-v0.2.24-darwin-arm64.tar.gz"
      sha256 "e662a9eb2d6f2a02514d9044468d38ee70dcc4542192384b7a150bef03616aae"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.24/xissue-v0.2.24-darwin-amd64.tar.gz"
      sha256 "47514f14cf3b88788ebbfb80e89b56644381f913d8d87d68a928ae73743a78eb"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.24/xissue-v0.2.24-linux-arm64.tar.gz"
      sha256 "c32138556400f52e4db0966368d61303898f2f2a5f137998166d4b2a9f0c2d3a"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.24/xissue-v0.2.24-linux-amd64.tar.gz"
      sha256 "75f530aae1b94134f61352921af3dce78c3b306651897471d3d4d97f92377274"
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
