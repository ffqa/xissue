class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.2"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.2/xissue-v0.8.2-darwin-arm64.tar.gz"
      sha256 "cdb6bed5efa8b029ada0a396ca23794278fffbb3afd776ca6000b66ae64effd1"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.2/xissue-v0.8.2-darwin-amd64.tar.gz"
      sha256 "5e582e5b9f88bec57dbf4331ee3c0804bcdeeb9ed05e140af193dae78d4de410"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.2/xissue-v0.8.2-linux-arm64.tar.gz"
      sha256 "10c7d7396c6b6d6a41267195f8633084c7754989944bdd1fa8a7aac7a274f3a4"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.2/xissue-v0.8.2-linux-amd64.tar.gz"
      sha256 "47d47c36d92381d02a9e522cbf6cb6fa4a3e1b021161b6534fdb90abbe8b1473"
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
