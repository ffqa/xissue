class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.31"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.31/xissue-v0.2.31-darwin-arm64.tar.gz"
      sha256 "7ae339ffa11de64dad23eac3f47e7835c442a907b0f5f1102977e9d8ce5b3099"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.31/xissue-v0.2.31-darwin-amd64.tar.gz"
      sha256 "4588302c450992917ff9375c9ce7644ff1e2a4b75fa6a4980c55c7615b96a5b8"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.31/xissue-v0.2.31-linux-arm64.tar.gz"
      sha256 "f1890720e32272e3be38fbd62568dfec997b9b103f6df0d20cbd113ac4117269"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.31/xissue-v0.2.31-linux-amd64.tar.gz"
      sha256 "1b060df032c2dc2a9cb0ca9966bcfec735a493b9bb60693d71673cfbea4c853c"
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
