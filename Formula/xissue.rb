class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.15"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.15/xissue-v0.8.15-darwin-arm64.tar.gz"
      sha256 "7266cb8818665fe086a2089f7491b4db79724a502f62b66ef2d7b8ad00e0b5e9"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.15/xissue-v0.8.15-darwin-amd64.tar.gz"
      sha256 "dfb3fe78a35a8456f8f073fa2e90058885a5b2e07aed1155c4b909fa0e9ca8b8"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.15/xissue-v0.8.15-linux-arm64.tar.gz"
      sha256 "a98160ae2d145e24e7e0736de7f0ab67809b006d2c1d8176028635458dd50bb6"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.15/xissue-v0.8.15-linux-amd64.tar.gz"
      sha256 "fdcad065f18951ab81f56493f748034406b31023434e04115a920605f71ebb5d"
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
