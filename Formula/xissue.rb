class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.5.9"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.9/xissue-v0.5.9-darwin-arm64.tar.gz"
      sha256 "3cb536d7d67e41fc0c5ba7516742b2a41644cdb377725af63054044e7c1e7da4"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.9/xissue-v0.5.9-darwin-amd64.tar.gz"
      sha256 "244aa330ccd6b9b0e89efb1ab6bdc6f100b9feb9ad369b14b6779ed71802542a"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.9/xissue-v0.5.9-linux-arm64.tar.gz"
      sha256 "d4cbfcc1c1d32f482bd484b1c3a0ced80fc79f6c3136e200ca285e89b2cadfdf"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.9/xissue-v0.5.9-linux-amd64.tar.gz"
      sha256 "856cfc3f0015e0c7f463eefdf4006fa2af332326027c5a240b10a5e5c8503631"
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
