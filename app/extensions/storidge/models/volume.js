export function StoridgeVolumeModel(data) {
  this.Allocated = data['alloc percent'];
  this.Capacity = data.capacity;
  this.Directory = data.directory;
  this.IOPSMax = data.iopsmax;
  this.IOPSMin = data.iopsmin;
  this.LocalDriveOnly = data.localdriveonly;
  this.Name = data.name;
  this.Node = data.node;
  this.NodeID = data.nodeid;
  this.Provisioning = data.provisioning;
  this.Redundancy = data.redundancy;
  this.Uuid = data.uuid;
  this.Vdisk = data.vdisk;
  this.Labels = data.labels;

  this.IP = data.ipaddr;
  this.DriveType = data.type;
  this.Encryption = data.encryption;
  this.SnapshotEnabled = data.snapshot;
  this.SnapshotInterval = data.snapinterval;
  this.SnapshotMax = data.snapmax;
  this.Filesystem = data.filesystem;
}

export function StoridgeVolumeUpdateModel(data) {
  this.name = data.Name;
  this.opts = {
    node: data.Node,
    nodeid: data.NodeID,
    capacity: data.Capacity,
    iopsmin: data.IOPSMin,
    iopsmax: data.IOPSMax,
    bandwidthmin: data.BandwidthMin,
    bandwidthmax: data.BandwidthMax
  };
  this.labels = data.Labels;
}