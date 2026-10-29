class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.5.11"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.11/xissue-v0.5.11-darwin-arm64.tar.gz"
      sha256 "1a942be58e952e3c0d567d0939b9c923a6fe102a3ef45a36ba4b3c52f4fefbe0"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.11/xissue-v0.5.11-darwin-amd64.tar.gz"
      sha256 "c3319b68a73af6a48752f1720b30a1fcf8bff58e9265b39572600dac3d92b83e"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.11/xissue-v0.5.11-linux-arm64.tar.gz"
      sha256 "106cd0c1eb7834ac2a8806ca92bc181bd5391df76bc745bbecb0e609bbf89c36"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.11/xissue-v0.5.11-linux-amd64.tar.gz"
      sha256 "841d7d57a51eb74f8e022957501fd2777d2c9ea53cfeeca0e4efd12db6860ee8"
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
