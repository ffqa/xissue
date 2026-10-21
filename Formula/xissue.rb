class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.12"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.12/xissue-v0.2.12-darwin-arm64.tar.gz"
      sha256 "dc74cb381f17fa9c0ed64fcc26dd1825ad0ed906b216fa407488a995301c07bb"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.12/xissue-v0.2.12-darwin-amd64.tar.gz"
      sha256 "dded241cc83da902dfb9fe6d5ce8d74729f85a64e9cbe79590760caa363e772b"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.12/xissue-v0.2.12-linux-arm64.tar.gz"
      sha256 "8f6162ff7bb8ee094f4c392bd9307e612885c97f78fb123263c6deb9dd4284fc"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.12/xissue-v0.2.12-linux-amd64.tar.gz"
      sha256 "3c1a3c01b00f65db67147c47f5af14d0de534534cb0efb3f07ab2df50bf0c14b"
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
