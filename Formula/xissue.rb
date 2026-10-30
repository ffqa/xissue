class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.5.14"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.14/xissue-v0.5.14-darwin-arm64.tar.gz"
      sha256 "c90fceecee34d2cd0a791d6c929808502d2feeedf5b3d43d87126ea3d02f7673"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.14/xissue-v0.5.14-darwin-amd64.tar.gz"
      sha256 "9301332de9e32996ee3e48ae10bd1e06f59d00234714da123822d246771633ad"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.14/xissue-v0.5.14-linux-arm64.tar.gz"
      sha256 "7a7292712c137674018b55d2e7a04642b774b961d41e65ec204e01dc4d6f6fdd"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.14/xissue-v0.5.14-linux-amd64.tar.gz"
      sha256 "81478db8661799c9cc53f0997827b2f851ce6319904642142bc1c063763fc6be"
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
