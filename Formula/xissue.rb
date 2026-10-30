class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.5.16"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.16/xissue-v0.5.16-darwin-arm64.tar.gz"
      sha256 "c246f38def2816bd232bf64385b170ef9325684e93602582f76e4dc4bc81a84a"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.16/xissue-v0.5.16-darwin-amd64.tar.gz"
      sha256 "52c33e92e61fd1c1a091930820ddd71688ea0653ba956c508396555cc3b32ec7"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.16/xissue-v0.5.16-linux-arm64.tar.gz"
      sha256 "1734ee92bc11f0b6f3fdaf3fb42ab6dcfa8cfcf864a5c5cb416219e8e23216ca"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.16/xissue-v0.5.16-linux-amd64.tar.gz"
      sha256 "df464fe4b07d88172dd92d9f13b354508b02d841247121edcc58977cdbc491d6"
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
