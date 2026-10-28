class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.3.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.3.0/xissue-v0.3.0-darwin-arm64.tar.gz"
      sha256 "8d56911ffa0a39c6d641b4c81eb2df991924b67e329941e4c370ae31c26e16ad"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.3.0/xissue-v0.3.0-darwin-amd64.tar.gz"
      sha256 "ec2168f20b04241de397dcc0d86f65f053d0cf551d6348bb7749cf270a9813e4"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.3.0/xissue-v0.3.0-linux-arm64.tar.gz"
      sha256 "a65d0b211979a337e54afb7b82f9b1fba1686b800e338e8fbc97abbdb29192d1"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.3.0/xissue-v0.3.0-linux-amd64.tar.gz"
      sha256 "2e1c44764ddbe6e450e624c663ce0b129d8e4d619d4119198a6c13a87f0373db"
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
