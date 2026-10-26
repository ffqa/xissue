class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.30"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.30/xissue-v0.2.30-darwin-arm64.tar.gz"
      sha256 "ab628cc3a18f2225fa26cd5eff7cc5804cea92bb1a8bff8802e660d46a7cba70"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.30/xissue-v0.2.30-darwin-amd64.tar.gz"
      sha256 "c5a57f6c2be9ef1d3019677563ece7f2efb129fc4081e8a20f2107ff32a3f5eb"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.30/xissue-v0.2.30-linux-arm64.tar.gz"
      sha256 "28704c227ba2711ce42bad4eb8ec7df82b20e4cdc2514bbe0e7c2503b631b5e7"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.30/xissue-v0.2.30-linux-amd64.tar.gz"
      sha256 "d67f765f953d89e9786d391a4d66aa4fcaf1e5808dc7d1c8aebcc1c22ac20e8a"
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
