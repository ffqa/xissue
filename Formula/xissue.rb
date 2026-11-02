class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.5"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.5/xissue-v0.8.5-darwin-arm64.tar.gz"
      sha256 "74a4a6a6b48b6abb34c5397caccd8240ff50c0256c5494b4236e9e238add3036"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.5/xissue-v0.8.5-darwin-amd64.tar.gz"
      sha256 "e9d232c7206faab2ae839a1969a92e6c5ec3d335e74c8c5bd5b785d494b7ed6b"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.5/xissue-v0.8.5-linux-arm64.tar.gz"
      sha256 "db1b12f6996c774822cfa3ed396b9253a4eca47db57bad45f88eb65308ddb87b"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.5/xissue-v0.8.5-linux-amd64.tar.gz"
      sha256 "eb76ef5a84e66214335a79bcb239c03098a96235be2252bb5770b1d9f0f76684"
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
