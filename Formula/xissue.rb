class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.34"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.34/xissue-v0.2.34-darwin-arm64.tar.gz"
      sha256 "6ce8d11708b174ae0f5cb1872c0d42829006ce196efcfdcd9088da28cdf60245"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.34/xissue-v0.2.34-darwin-amd64.tar.gz"
      sha256 "01d45042c9fcd5142bdbfb31b45089b7c94e0e3f61350bd42d572167b7655238"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.34/xissue-v0.2.34-linux-arm64.tar.gz"
      sha256 "2c223a650bf9183bbb9db78e97ca43b2cc4cd212ef4766d85598724ce62087c1"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.34/xissue-v0.2.34-linux-amd64.tar.gz"
      sha256 "f29b3aff1a1a870d891a6a570c48648dbd1fc92424bb759b7edb8a324468c969"
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
