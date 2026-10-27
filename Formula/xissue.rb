class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.39"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.39/xissue-v0.2.39-darwin-arm64.tar.gz"
      sha256 "edfd63091f1f6a49b035a6f2c2a117322847675d4c1dd58ffacd43bb7be7afb1"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.39/xissue-v0.2.39-darwin-amd64.tar.gz"
      sha256 "4379af3fa078b48be9e90da0c993dfe83cc98ac2319bf700df8409de9d8d2c14"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.39/xissue-v0.2.39-linux-arm64.tar.gz"
      sha256 "797fe7306a3ff3a14b59e6796726da58e9df56615c8bbc5a36ea5b0d0e729212"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.39/xissue-v0.2.39-linux-amd64.tar.gz"
      sha256 "5caf4b65a0e7b150d6087a0a142d35d393046abf6ebf6ef9341df694cb3183d5"
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
