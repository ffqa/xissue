class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.5.4"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.4/xissue-v0.5.4-darwin-arm64.tar.gz"
      sha256 "4102b756ee1ee41a58a7294f4ca9f9ed3fab919108b521af4778746c8dfb2ce6"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.4/xissue-v0.5.4-darwin-amd64.tar.gz"
      sha256 "62372714f1b4f783935ef47f6f22275e00d8081a0a24a81d45d5c8da397b91db"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.4/xissue-v0.5.4-linux-arm64.tar.gz"
      sha256 "52af83151b32200bf03bd44bb6b5f2f1a9c8e2578b8d143ea5fd38d271f4bce4"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.4/xissue-v0.5.4-linux-amd64.tar.gz"
      sha256 "a00e54b83fb0dc1b06228cfb86998e973144ff43499c7a41eecc18a645488aef"
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
